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
