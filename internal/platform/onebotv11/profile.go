package onebotv11

import (
	"fmt"
	"strings"
)

type Profile struct {
	Name                string
	DefaultInputFormat  string
	DefaultOutputFormat string
	SupportsMarkdown    bool
	SupportsJSONSegment bool
	RetcodeSuccess      func(ActionResponse) bool
	NormalizeEvent      func(*Event)
	NormalizeSegment    func(Segment) Segment
}

func ProfileGeneric() Profile {
	return Profile{
		Name:                "generic",
		DefaultInputFormat:  MessageFormatArray,
		DefaultOutputFormat: MessageFormatArray,
		RetcodeSuccess:      standardRetcodeSuccess,
	}
}

func ProfileNapCat() Profile {
	p := ProfileGeneric()
	p.Name = "napcat"
	p.SupportsJSONSegment = true
	return p
}

func ProfileSnowLuma() Profile {
	p := ProfileGeneric()
	p.Name = "snowluma"
	p.SupportsJSONSegment = true
	return p
}

func SelectProfile(name string) (Profile, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "generic":
		return ProfileGeneric(), nil
	case "napcat":
		return ProfileNapCat(), nil
	case "snowluma":
		return ProfileSnowLuma(), nil
	default:
		return Profile{}, fmt.Errorf("implementation must be generic, napcat, or snowluma")
	}
}

func standardRetcodeSuccess(resp ActionResponse) bool {
	return strings.EqualFold(resp.Status, "ok") && resp.Retcode == 0
}
