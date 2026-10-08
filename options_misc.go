package validate

import (
	"errors"
	"sort"
)

func asRefine(err error, target **RefineError) bool { return errors.As(err, target) }

func asRequestError(err error) (*RequestError, bool) {
	var re *RequestError
	ok := errors.As(err, &re)
	return re, ok
}

func sortStrings(s []string) { sort.Strings(s) }
