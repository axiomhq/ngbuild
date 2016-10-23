package core

import (
	"errors"
	"regexp"
)

var (
	errNoMatch = errors.New("Could not match regexp")
)

// RegexpNamedGroupsMatch - will for a given regexp and search give you a a map of named groups to found values
func RegexpNamedGroupsMatch(pattern *regexp.Regexp, search string) (namedGroupMatch map[string]string, err error) {
	if pattern.MatchString(search) == false {
		err = errNoMatch
		return
	}
	namedGroupMatch = make(map[string]string)
	groups := pattern.SubexpNames()
	for _, group := range groups {
		if group == "" {
			continue
		}
		namedGroupMatch[group] = ""
	}

	for index, submatch := range pattern.FindStringSubmatch(search) {
		// first returned value is just the entire string, which is totally useful. skip it.
		if index < 1 {
			continue
		}
		namedGroupMatch[groups[index]] = submatch
	}

	return
}
