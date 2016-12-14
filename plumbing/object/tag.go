package object

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	stdioutil "io/ioutil"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
	"gopkg.in/src-d/go-git.v4/utils/ioutil"
)

// Tag represents an annotated tag object. It points to a single git object of
// any type, but tags typically are applied to commit or blob objects. It
// provides a reference that associates the target with a tag name. It also
// contains meta-information about the tag, including the tagger, tag date and
// message.
//
// https://git-scm.com/book/en/v2/Git-Internals-Git-References#Tags
type Tag struct {
	Hash       plumbing.Hash
	Name       string
	Tagger     Signature
	Message    string
	TargetType plumbing.ObjectType
	Target     plumbing.Hash

	s storer.EncodedObjectStorer
}

// GetTag gets a tag from an object storer and decodes it.
func GetTag(s storer.EncodedObjectStorer, h plumbing.Hash) (*Tag, error) {
	o, err := s.EncodedObject(plumbing.TagObject, h)
	if err != nil {
		return nil, err
	}

	return DecodeTag(s, o)
}

// DecodeTag decodes an encoded object into a *Commit and associates it to the
// given object storer.
func DecodeTag(s storer.EncodedObjectStorer, o plumbing.EncodedObject) (*Tag, error) {
	t := &Tag{s: s}
	if err := t.Decode(o); err != nil {
		return nil, err
	}

	return t, nil
}

// ID returns the object ID of the tag, not the object that the tag references.
// The returned value will always match the current value of Tag.Hash.
//
// ID is present to fulfill the Object interface.
func (t *Tag) ID() plumbing.Hash {
	return t.Hash
}

// Type returns the type of object. It always returns plumbing.TagObject.
//
// Type is present to fulfill the Object interface.
func (t *Tag) Type() plumbing.ObjectType {
	return plumbing.TagObject
}

// Decode transforms a plumbing.EncodedObject into a Tag struct.
func (t *Tag) Decode(o plumbing.EncodedObject) (err error) {
	if o.Type() != plumbing.TagObject {
		return ErrUnsupportedObject
	}

	t.Hash = o.Hash()

	reader, err := o.Reader()
	if err != nil {
		return err
	}
	defer ioutil.CheckClose(reader, &err)

	r := bufio.NewReader(reader)
	for {
		line, err := r.ReadSlice('\n')
		if err != nil && err != io.EOF {
			return err
		}

		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			break // Start of message
		}

		split := bytes.SplitN(line, []byte{' '}, 2)
		switch string(split[0]) {
		case "object":
			t.Target = plumbing.NewHash(string(split[1]))
		case "type":
			t.TargetType, err = plumbing.ParseObjectType(string(split[1]))
			if err != nil {
				return err
			}
		case "tag":
			t.Name = string(split[1])
		case "tagger":
			t.Tagger.Decode(split[1])
		}

		if err == io.EOF {
			return nil
		}
	}

	data, err := stdioutil.ReadAll(r)
	if err != nil {
		return err
	}
	t.Message = string(data)

	return nil
}

// Encode transforms a Tag into a plumbing.EncodedObject.
func (t *Tag) Encode(o plumbing.EncodedObject) error {
	o.SetType(plumbing.TagObject)
	w, err := o.Writer()
	if err != nil {
		return err
	}
	defer ioutil.CheckClose(w, &err)

	if _, err = fmt.Fprintf(w,
		"object %s\ntype %s\ntag %s\ntagger ",
		t.Target.String(), t.TargetType.Bytes(), t.Name); err != nil {
		return err
	}

	if err = t.Tagger.Encode(w); err != nil {
		return err
	}

	if _, err = fmt.Fprint(w, "\n\n"); err != nil {
		return err
	}

	if _, err = fmt.Fprint(w, t.Message); err != nil {
		return err
	}

	return err
}

// Commit returns the commit pointed to by the tag. If the tag points to a
// different type of object ErrUnsupportedObject will be returned.
func (t *Tag) Commit() (*Commit, error) {
	if t.TargetType != plumbing.CommitObject {
		return nil, ErrUnsupportedObject
	}

	o, err := t.s.EncodedObject(plumbing.CommitObject, t.Target)
	if err != nil {
		return nil, err
	}

	return DecodeCommit(t.s, o)
}

// Tree returns the tree pointed to by the tag. If the tag points to a commit
// object the tree of that commit will be returned. If the tag does not point
// to a commit or tree object ErrUnsupportedObject will be returned.
func (t *Tag) Tree() (*Tree, error) {
	switch t.TargetType {
	case plumbing.CommitObject:
		c, err := t.Commit()
		if err != nil {
			return nil, err
		}

		return c.Tree()
	case plumbing.TreeObject:
		return GetTree(t.s, t.Target)
	default:
		return nil, ErrUnsupportedObject
	}
}

// Blob returns the blob pointed to by the tag. If the tag points to a
// different type of object ErrUnsupportedObject will be returned.
func (t *Tag) Blob() (*Blob, error) {
	if t.TargetType != plumbing.BlobObject {
		return nil, ErrUnsupportedObject
	}

	return GetBlob(t.s, t.Target)
}

// Object returns the object pointed to by the tag.
func (t *Tag) Object() (Object, error) {
	o, err := t.s.EncodedObject(t.TargetType, t.Target)
	if err != nil {
		return nil, err
	}

	return DecodeObject(t.s, o)
}

// String returns the meta information contained in the tag as a formatted
// string.
func (t *Tag) String() string {
	obj, _ := t.Object()

	return fmt.Sprintf(
		"%s %s\nTagger: %s\nDate:   %s\n\n%s\n%s",
		plumbing.TagObject, t.Name, t.Tagger.String(), t.Tagger.When.Format(DateFormat),
		t.Message, objectAsString(obj),
	)
}

// TagIter provides an iterator for a set of tags.
type TagIter struct {
	storer.EncodedObjectIter
	s storer.EncodedObjectStorer
}

// NewTagIter returns a TagIter for the given object storer and underlying
// object iterator.
//
// The returned TagIter will automatically skip over non-tag objects.
func NewTagIter(s storer.EncodedObjectStorer, iter storer.EncodedObjectIter) *TagIter {
	return &TagIter{iter, s}
}

// Next moves the iterator to the next tag and returns a pointer to it. If it
// has reached the end of the set it will return io.EOF.
func (iter *TagIter) Next() (*Tag, error) {
	obj, err := iter.EncodedObjectIter.Next()
	if err != nil {
		return nil, err
	}

	return DecodeTag(iter.s, obj)
}

// ForEach call the cb function for each tag contained on this iter until
// an error happends or the end of the iter is reached. If ErrStop is sent
// the iteration is stop but no error is returned. The iterator is closed.
func (iter *TagIter) ForEach(cb func(*Tag) error) error {
	return iter.EncodedObjectIter.ForEach(func(obj plumbing.EncodedObject) error {
		t, err := DecodeTag(iter.s, obj)
		if err != nil {
			return err
		}

		return cb(t)
	})
}

func objectAsString(obj Object) string {
	switch o := obj.(type) {
	case *Commit:
		return o.String()
	case *Tag:
		return o.String()
	}

	return ""
}