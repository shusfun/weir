package subscriptions

// Name metadata follows fixed MiSub 4952c16 metadata-extractor.js (MIT).
// It is a naming hint, never an accounting multiplier or location measurement.
import (
	"regexp"
	"strconv"
	"strings"
)

type NodeNameMetadata struct {
	OriginalName string  `json:"originalName"`
	CleanName    string  `json:"cleanName"`
	Multiplier   float64 `json:"multiplier"`
}

// The upstream character class includes literal x as well as [ and (.
var multiplierPattern = regexp.MustCompile(`(?i)(?:^|\s|[\[(x])([0-9]+(?:\.[0-9]+)?)(?:\s*倍|x)(?:\s|$|[\])])`)
var flagPattern = regexp.MustCompile(`[\x{1F1E6}-\x{1F1FF}]{2}`)
var metadataSpace = regexp.MustCompile(`\s{2,}`)

func ExtractNodeMetadata(name string) NodeNameMetadata {
	m := NodeNameMetadata{OriginalName: name, CleanName: name, Multiplier: 1}
	if match := multiplierPattern.FindStringSubmatchIndex(name); match != nil {
		if v, e := strconv.ParseFloat(name[match[2]:match[3]], 64); e == nil {
			m.Multiplier = v
		}
		m.CleanName = strings.TrimSpace(name[:match[0]] + " " + name[match[1]:])
	}
	m.CleanName = flagPattern.ReplaceAllString(m.CleanName, "")
	m.CleanName = strings.NewReplacer("[", "", "]", "", "(", "", ")", "", "#", "").Replace(m.CleanName)
	m.CleanName = strings.TrimSpace(metadataSpace.ReplaceAllString(m.CleanName, " "))
	if m.CleanName == "" {
		m.CleanName = name
	}
	return m
}
