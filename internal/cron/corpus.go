package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/pkg/utils"
)

// LoadCorpus walks dir for *.json under the gmail/, linkedin/ and djinni/
// subdirectories and converts each file's entries into typed brain leafs.
// Unknown subdirectories and unparseable files are skipped, never fatal.
func LoadCorpus(dir string) ([]Leaf, error) {
	var leafs []Leaf
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".json") {
			return err
		}
		src, ok := sourceFromPath(p)
		if !ok {
			return nil // outside gmail/linkedin/djinni — skip
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		leafs = append(leafs, convert(src, filepath.Base(p), b)...)
		return nil
	})
	return leafs, err
}

func sourceFromPath(p string) (string, bool) {
	switch filepath.Base(filepath.Dir(p)) {
	case "gmail", "linkedin", "djinni":
		return filepath.Base(filepath.Dir(p)), true
	}
	return "", false
}

func convert(src, base string, b []byte) []Leaf {
	switch src {
	case "gmail":
		return convertGmail(b)
	case "linkedin":
		switch base {
		case "connections.json":
			return convertLinkedinConnections(b)
		case "profile.json":
			return convertLinkedinProfile(b)
		}
	case "djinni":
		switch base {
		case "applications.json":
			return convertDjinniApplications(b)
		case "dashboard.json":
			return convertDjinniDashboard(b)
		case "messages.json":
			return convertDjinniMessages(b)
		case "profile.json":
			return convertDjinniProfile(b)
		}
	}
	return nil
}

// leaf builds an info-rooted, confirmed leaf observed via the browser.
func leaf(text, source, typ string) Leaf {
	return Leaf{Text: text, Source: source, Root: "info", Confidence: "confirmed", Type: typ}
}

func convertGmail(b []byte) []Leaf {
	var f struct {
		Account string
		Emails  []struct {
			Sender   string
			Subject  string
			ThreadID string `json:"thread_id"`
		}
	}
	if json.Unmarshal(b, &f) != nil || len(f.Emails) == 0 {
		return nil
	}
	out := make([]Leaf, 0, len(f.Emails))
	for _, e := range f.Emails {
		from := utils.Or(e.Sender, "unknown")
		text := fmt.Sprintf("Gmail (%s): email from %s — %s", f.Account, from, e.Subject)
		src := "gmail:" + utils.Or(f.Account, "account") + ":" + utils.Or(e.ThreadID, e.Subject)
		out = append(out, leaf(text, src, "email"))
	}
	return out
}

func convertLinkedinConnections(b []byte) []Leaf {
	var f struct {
		Page []struct{ Name, Headline string } `json:"page_1_connections"`
	}
	if json.Unmarshal(b, &f) != nil || len(f.Page) == 0 {
		return nil
	}
	out := make([]Leaf, 0, len(f.Page))
	for _, c := range f.Page {
		if c.Name == "" {
			continue
		}
		text := fmt.Sprintf("LinkedIn connection: %s — %s", c.Name, utils.Or(c.Headline, "no headline"))
		out = append(out, leaf(text, "linkedin:connection:"+c.Name, "connection"))
	}
	return out
}

func convertLinkedinProfile(b []byte) []Leaf {
	var f struct {
		Name, Headline, Location string
		Experience               []struct{ Role, Company, Dates string }
		Education                []struct{ Institution, Program, Dates string }
	}
	if json.Unmarshal(b, &f) != nil || f.Name == "" {
		return nil
	}
	out := []Leaf{leaf(
		fmt.Sprintf("LinkedIn profile: %s — %s (%s)", f.Name, f.Headline, utils.Or(f.Location, "location unknown")),
		"linkedin:profile:"+f.Name,
		"profile",
	)}
	for _, e := range f.Experience {
		if e.Role == "" {
			continue
		}
		text := fmt.Sprintf("LinkedIn experience: %s at %s (%s)", e.Role, utils.Or(e.Company, "?"), utils.Or(e.Dates, "dates unknown"))
		out = append(out, leaf(text, "linkedin:experience:"+e.Role+":"+utils.Or(e.Company, "?"), "experience"))
	}
	for _, e := range f.Education {
		if e.Institution == "" {
			continue
		}
		text := fmt.Sprintf("LinkedIn education: %s — %s (%s)", e.Institution, utils.Or(e.Program, "?"), utils.Or(e.Dates, "dates unknown"))
		out = append(out, leaf(text, "linkedin:education:"+e.Institution, "education"))
	}
	return out
}

func convertDjinniDashboard(b []byte) []Leaf {
	var f struct {
		RecommendedJobs []struct {
			Title, Company, Salary, Location, Experience, English, Summary string
		} `json:"recommended_jobs"`
		Stats struct {
			PropositionsReceived int `json:"propositions_received"`
			ApplicationsSent     int `json:"applications_sent"`
			ProfileViews         int `json:"profile_views"`
		} `json:"statistics_30d"`
	}
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	var out []Leaf
	for _, j := range f.RecommendedJobs {
		if j.Title == "" {
			continue
		}
		text := fmt.Sprintf("Djinni recommended: %s at %s (%s, %s, %s)", j.Title,
			utils.Or(j.Company, "?"), utils.Or(j.Location, "?"), utils.Or(j.Experience, "?"), utils.Or(j.English, "?"))
		out = append(out, leaf(text, "djinni:recommended:"+j.Title+":"+utils.Or(j.Company, "?"), "job"))
	}
	if f.Stats.PropositionsReceived > 0 || f.Stats.ApplicationsSent > 0 || f.Stats.ProfileViews > 0 {
		text := fmt.Sprintf("Djinni 30d stats: propositions=%d applications=%d profile_views=%d",
			f.Stats.PropositionsReceived, f.Stats.ApplicationsSent, f.Stats.ProfileViews)
		out = append(out, leaf(text, "djinni:stats:30d", "stats"))
	}
	return out
}

func convertDjinniMessages(b []byte) []Leaf {
	var f struct {
		Threads []struct{ Company, Contact, Subject, Status string }
	}
	if json.Unmarshal(b, &f) != nil || len(f.Threads) == 0 {
		return nil
	}
	out := make([]Leaf, 0, len(f.Threads))
	for _, th := range f.Threads {
		if th.Subject == "" {
			continue
		}
		text := fmt.Sprintf("Djinni inbox: %s (%s) — %s — %s",
			utils.Or(th.Contact, "?"), utils.Or(th.Company, "?"), th.Subject, utils.Or(th.Status, "?"))
		out = append(out, leaf(text, "djinni:inbox:"+th.Subject+":"+utils.Or(th.Company, "?"), "message"))
	}
	return out
}

func convertDjinniApplications(b []byte) []Leaf {
	var f struct {
		Entries []struct {
			Type, Status, Title, Company, Location, Summary string
		}
	}
	if json.Unmarshal(b, &f) != nil || len(f.Entries) == 0 {
		return nil
	}
	out := make([]Leaf, 0, len(f.Entries))
	for _, e := range f.Entries {
		if e.Title == "" {
			continue
		}
		kind := utils.Or(e.Type, "application")
		text := fmt.Sprintf("Djinni %s: %s at %s (%s, %s)", kind, e.Title,
			utils.Or(e.Company, "?"), utils.Or(e.Status, "?"), utils.Or(e.Location, "?"))
		out = append(out, leaf(text, "djinni:"+kind+":"+e.Title+":"+utils.Or(e.Company, "?"), kind))
	}
	return out
}

func convertDjinniProfile(b []byte) []Leaf {
	var f struct {
		Name, Title string
		Skills      []struct {
			Skill string
			Years int
		}
		Summary struct {
			CurrentRole string `json:"current_role"`
		} `json:"experience_summary"`
	}
	if json.Unmarshal(b, &f) != nil || f.Name == "" {
		return nil
	}
	out := []Leaf{leaf(
		fmt.Sprintf("Djinni profile: %s — %s", f.Name, utils.Or(f.Title, "?")),
		"djinni:profile:"+f.Name,
		"profile",
	)}
	if f.Summary.CurrentRole != "" {
		out = append(out, leaf(
			"Djinni current role: "+f.Summary.CurrentRole,
			"djinni:profile:"+f.Name+":current_role",
			"profile",
		))
	}
	if len(f.Skills) > 0 {
		var sb strings.Builder
		sb.WriteString("Djinni skills:")
		for _, s := range f.Skills {
			fmt.Fprintf(&sb, " %s(%dy);", s.Skill, s.Years)
		}
		out = append(out, leaf(sb.String(), "djinni:profile:"+f.Name+":skills", "profile"))
	}
	return out
}
