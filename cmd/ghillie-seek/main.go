// Command ghillie-seek is the JOBSEEKER'S side of the gated CV: the owner's
// document held locally, and every enquiry answered through the proven
// disclosure ladder (Cv_Disclosure_Pkg via CV_DISCLOSURE_DECIDER).
//
// THE PRODUCT IS THE REFUSAL, AND THE RECORD. Nothing personal is released
// until the enquirer has ESTABLISHED conditions — a named end client, a real
// vacancy, a stated salary — and every enquiry, released or withheld, lands
// in an append-only record. The owner can always answer exactly the question
// nobody currently can: where did my details go?
//
// NO LISTENER, NO NETWORK. An enquiry arrives as a FILE (an ask the offerer
// side composed — see ghillie-offer) and the answer leaves as a file. The
// claw-to-claw transport is later machinery; the ladder and the record are
// the product and they are complete here.
//
// Usage (zsh):
//
//	ghillie-seek -init-cv                      # write the CV template (owner act)
//	ghillie-seek -ask ask.json                 # answer one enquiry
//	ghillie-seek -ask ask.json -owner-consent  # ...with the owner's personal yes (tier 4)
//	ghillie-seek -record                       # show the disclosure record
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/tonygair/ghillie/internal/cvgate"
)

// Ask is what an enquirer states about itself and its request. Conditions are
// CLAIMS RECORDED, not promises believed — the record keeps them beside what
// they earned.
type Ask struct {
	Enquirer     string `json:"enquirer"`
	EndClient    string `json:"end_client"`    // "" = not named (the most-refused thing in recruitment)
	VacancyRef   string `json:"vacancy_ref"`   // "" = no identified role
	Salary       string `json:"salary"`        // "" = "competitive", which is nothing
	NoForwarding bool   `json:"no_forwarding"` // agreed not to pass details on
}

// Answer is what one enquiry yielded — the answer AND the ledger row.
type Answer struct {
	Enquirer   string            `json:"enquirer"`
	AskedAt    string            `json:"asked_at"`
	Conditions Ask               `json:"conditions_as_stated"`
	Released   map[string]string `json:"released"`
	Withheld   map[string]string `json:"withheld"` // field -> the actionable reason
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("ghillie-seek │ ")

	home, _ := os.UserHomeDir()
	defaultDir := filepath.Join(home, ".ghillie", "seek")

	var (
		dir          = flag.String("home", defaultDir, "where the CV, answers and the disclosure record live (0600/0700, never /tmp)")
		initCV       = flag.Bool("init-cv", false, "write a CV template for the owner to fill in, then exit — AN OWNER ACT")
		askFile      = flag.String("ask", "", "answer the enquiry in this ask.json (see ghillie-offer -compose)")
		ownerConsent = flag.Bool("owner-consent", false, "the OWNER'S personal yes to THIS request — the only route to tier-4 fields, never inferred from conditions")
		showRecord   = flag.Bool("record", false, "print the disclosure record — every enquiry, what left, what was withheld and why")
	)
	flag.Parse()

	if err := run(*dir, *initCV, *askFile, *ownerConsent, *showRecord); err != nil {
		fmt.Fprintf(os.Stderr, "ghillie-seek: %v\n", err)
		os.Exit(1)
	}
}

func run(dir string, initCV bool, askFile string, ownerConsent, showRecord bool) error {
	cvPath := filepath.Join(dir, "cv.json")
	recordPath := filepath.Join(dir, "disclosure-record.jsonl")

	switch {
	case initCV:
		return writeTemplate(dir, cvPath)
	case showRecord:
		return printRecord(recordPath)
	case askFile != "":
		return answerAsk(dir, cvPath, recordPath, askFile, ownerConsent)
	}
	flag.Usage()
	return fmt.Errorf("one of -init-cv, -ask, -record is required")
}

func writeTemplate(dir, cvPath string) error {
	if _, err := os.Stat(cvPath); err == nil {
		return fmt.Errorf("%s already exists — edit it rather than overwriting; deleting a CV is the owner's act done by hand", cvPath)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	template := map[string]string{
		"sector": "", "seniority": "", "region": "", "years": "", "skills": "",
		"surname": "", "postcode": "", "email": "", "phone": "",
		"ni-number": "", "referee-name": "", "referee-phone": "",
	}
	raw, err := json.MarshalIndent(template, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(cvPath, raw, 0o600); err != nil {
		return err
	}
	log.Printf("template written: %s — fill in what is true, leave blank what is not; blank fields are simply not held", cvPath)
	log.Printf("tier-0 fields (sector, seniority, region, years, skills) identify nobody and make the gate useful; everything else is earned by conditions")
	return nil
}

func answerAsk(dir, cvPath, recordPath, askFile string, ownerConsent bool) error {
	rawCV, err := os.ReadFile(cvPath)
	if err != nil {
		return fmt.Errorf("no CV at %s — run -init-cv first (an owner act): %w", cvPath, err)
	}
	var fields map[string]string
	if err := json.Unmarshal(rawCV, &fields); err != nil {
		return fmt.Errorf("cv.json unreadable: %w", err)
	}
	held := map[cvgate.Field]string{}
	for k, v := range fields {
		if v != "" {
			held[cvgate.Field(k)] = v
		}
	}

	rawAsk, err := os.ReadFile(askFile)
	if err != nil {
		return err
	}
	var ask Ask
	if err := json.Unmarshal(rawAsk, &ask); err != nil {
		return fmt.Errorf("ask unreadable: %w", err)
	}
	if ask.Enquirer == "" {
		return fmt.Errorf("the ask names no enquirer — an anonymous enquiry earns nothing and is not recorded against anyone; refused")
	}

	// Conditions are the CLAIMS AS STATED. Owner consent never travels in an
	// ask — it is this owner, at this keyboard, for this request.
	c := cvgate.Conditions{
		ClientNamed:  ask.EndClient != "",
		VacancyRef:   ask.VacancyRef != "",
		SalaryStated: ask.Salary != "",
		NoForwarding: ask.NoForwarding,
		OwnerConsent: ownerConsent,
	}

	d := cvgate.New(mapFields(held)).Answer(c)

	ans := Answer{
		Enquirer:   ask.Enquirer,
		AskedAt:    time.Now().UTC().Format(time.RFC3339),
		Conditions: ask,
		Released:   map[string]string{},
		Withheld:   map[string]string{},
	}
	for _, f := range d.ReleasedFields() {
		ans.Released[string(f)] = d.Released[f]
	}
	for _, f := range d.WithheldFields() {
		ans.Withheld[string(f)] = d.Withheld[f]
	}

	// THE RECORD FIRST. An answer that could leave without its record would
	// break the one promise this tool exists to keep — so refuse to answer
	// unrecorded, exactly as the bundle installer refuses the unledgered.
	if err := appendRecord(recordPath, ans); err != nil {
		return fmt.Errorf("the record would not take the row — refusing to answer unrecorded: %w", err)
	}

	outPath := filepath.Join(dir, "answer-"+ask.Enquirer+"-"+time.Now().UTC().Format("20060102-150405")+".json")
	rawOut, err := json.MarshalIndent(ans, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(outPath, rawOut, 0o600); err != nil {
		return err
	}

	log.Printf("enquiry from %s: %d field(s) released, %d withheld", ask.Enquirer, len(ans.Released), len(ans.Withheld))
	for _, f := range d.WithheldFields() {
		log.Printf("  withheld %-14s %s", f, d.Withheld[f])
	}
	log.Printf("answer: %s", outPath)
	log.Printf("recorded: %s — the owner can always see exactly what left", recordPath)
	return nil
}

func mapFields(in map[cvgate.Field]string) map[cvgate.Field]string { return in }

func appendRecord(path string, ans Answer) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := json.Marshal(ans)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s\n", raw)
	return err
}

func printRecord(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("no enquiries recorded yet — nothing has ever left")
			return nil
		}
		return err
	}
	os.Stdout.Write(raw)
	return nil
}
