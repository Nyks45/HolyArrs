package sources

// queryErr is the error behind ErrQuery.
type queryErr struct{}

func (queryErr) Error() string {
	return "leave the query (?apikey=...) out of the address; put the key in the key field"
}

func (queryErr) Is(t error) bool { return t == ErrURL }
