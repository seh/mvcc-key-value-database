package db

type (
	// Key is the type of the primary record identifier used in the database.
	//
	// The database stores as many as one record for each unique key.
	Key []byte
	// Value is the type of payload stored by each record in the database.
	//
	// A record's value can be empty (a byte vector of length zero).
	Value []byte
)

func copyInto[V ~[]byte, U ~[]byte](dst *V, v U) int {
	length := len(v)
	if cap(*dst) < length {
		*dst = make([]byte, length)
	} else if len(*dst) != length {
		*dst = (*dst)[:length]
	}
	return copy(*dst, v)
}

// CopyFrom copies the content from the given other value into this value.
func (v *Value) CopyFrom(o Value) int {
	return copyInto(v, o)
}

// CopyInto copies the content from this value into the given other value, which must not be nil.
func (v Value) CopyInto(o *Value) int {
	return copyInto(o, v)
}
