package core

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testregexpmatch struct {
	pattern *regexp.Regexp
	search  string
	result  map[string]string
}

func TestRegexpmatchNamedGroupsMatch(t *testing.T) {
	testData := []testregexpmatch{
		testregexpmatch{
			pattern: regexp.MustCompile(`(?P<1>\d)(?P<2>\d)`),
			search:  "12",
			result: map[string]string{
				"1": "1",
				"2": "2",
			},
		},
		testregexpmatch{
			pattern: regexp.MustCompile(`(?P<1>\d)(?P<2>-)?(?P<3>\d)`),
			search:  "13",
			result: map[string]string{
				"1": "1",
				"2": "",
				"3": "3",
			},
		},
	}

	for _, testData := range testData {
		match, err := RegexpNamedGroupsMatch(testData.pattern, testData.search)
		assert.Nil(t, err)
		assert.Equal(t, testData.result, match)
	}
}
