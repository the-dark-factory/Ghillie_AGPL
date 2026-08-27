// Command ghillie-offer is the JOBOFFERER'S side of the gated CV: it composes
// a proper ask — the kind a seeker's gate rewards — and reads back what the
// conditions earned.
//
// THE GATE LOOKS LIKE COOPERATION FROM THIS SIDE. A recruiter who names the
// end client, identifies the vacancy, and states the salary gets more, not
// less: the ladder is published, the refusals are actionable, and asking
// properly is cheaper than asking twice. This tool makes composing the proper
// ask the easy path — and refuses to pretend an improper one will do better.
//
// NO NETWORK. An ask leaves as a file for the seeker's ghillie-seek; the
// answer comes back as a file. Claw-to-claw transport is later machinery.
//
// Usage (zsh):
//
//	ghillie-offer -compose -enquirer acme-recruitment \
//	              -end-client "Northern Grid plc" -vacancy-ref NG-2214 \
//	              -salary "£68k-74k" -no-forwarding -out ask.json
//	ghillie-offer -read answer.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
)

// Ask mirrors ghillie-seek's Ask — the two commands share a file format, not
// a wire. Kept in both places deliberately: each side owns its half and the
// JSON between them is the whole contract.
type Ask struct {
	Enquirer     string `json:"enquirer"`
	EndClient    string `json:"end_client"`
	VacancyRef   string `json:"vacancy_ref"`
	Salary       string `json:"salary"`
	NoForwarding bool   `json:"no_forwarding"`
}

// Answer mirrors ghillie-seek's Answer for reading.
type Answer struct {
	Enquirer   string            `json:"enquirer"`
	AskedAt    string            `json:"asked_at"`
	Conditions Ask               `json:"conditions_as_stated"`
	Released   map[string]string `json:"released"`
	Withheld   map[string]string `json:"withheld"`
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("ghillie-offer │ ")

	var (
		compose      = flag.Bool("compose", false, "compose an ask from the flags below, write it to -out, and say honestly what it will and will not earn")
		enquirer     = flag.String("enquirer", "", "who is asking — an anonymous enquiry earns nothing and seek refuses it (required with -compose)")
		endClient    = flag.String("end-client", "", "the REAL end client, named — not \"a leading firm in the sector\"; the single most-refused thing in recruitment, and the first rung of the ladder")
		vacancyRef   = flag.String("vacancy-ref", "", "a specific role that exists, identified")
		salary       = flag.String("salary", "", "a figure or a band — \"competitive\" is nothing and earns nothing")
		noForwarding = flag.Bool("no-forwarding", false, "agree not to pass the details on")
		out          = flag.String("out", "ask.json", "where the composed ask lands")
		read         = flag.String("read", "", "read an answer.json from the seeker's side and show what the conditions earned")
	)
	flag.Parse()

	if err := run(*compose, *enquirer, *endClient, *vacancyRef, *salary, *noForwarding, *out, *read); err != nil {
		fmt.Fprintf(os.Stderr, "ghillie-offer: %v\n", err)
		os.Exit(1)
	}
}

func run(compose bool, enquirer, endClient, vacancyRef, salary string, noForwarding bool, out, read string) error {
	switch {
	case compose:
		return composeAsk(enquirer, endClient, vacancyRef, salary, noForwarding, out)
	case read != "":
		return readAnswer(read)
	}
	flag.Usage()
	return fmt.Errorf("one of -compose or -read is required")
}

func composeAsk(enquirer, endClient, vacancyRef, salary string, noForwarding bool, out string) error {
	if enquirer == "" {
		return fmt.Errorf("-enquirer is required: an anonymous ask earns nothing and the seeker's gate refuses it outright")
	}
	ask := Ask{
		Enquirer: enquirer, EndClient: endClient, VacancyRef: vacancyRef,
		Salary: salary, NoForwarding: noForwarding,
	}
	raw, err := json.MarshalIndent(ask, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, raw, 0o600); err != nil {
		return err
	}
	log.Printf("ask written: %s", out)

	// Honesty at compose time: say what is missing IN THE LADDER'S OWN WORDS,
	// so asking properly is the path of least resistance. This is reporting,
	// not deciding — the seeker's proven gate holds the verdict, and if this
	// text and that verdict ever disagree, the verdict is right.
	var owed []string
	if endClient == "" {
		owed = append(owed, "name the end client (unlocks the surname tier — nothing personal moves without it)")
	}
	if vacancyRef == "" {
		owed = append(owed, "give the vacancy reference (required alongside the client name)")
	}
	if salary == "" {
		owed = append(owed, "state the salary or band (unlocks direct contact by email)")
	}
	if !noForwarding {
		owed = append(owed, "agree not to forward the details (unlocks the phone tier)")
	}
	if len(owed) == 0 {
		log.Printf("all four agency-side conditions stated — the ask earns everything conditions can earn")
		log.Printf("note: some fields are OWNER-ONLY and no set of conditions unlocks them; that is the ladder's top and it is not for sale")
	} else {
		log.Printf("this ask is honest but incomplete — the seeker's gate will withhold accordingly. Still owed:")
		for _, o := range owed {
			log.Printf("  · %s", o)
		}
	}
	return nil
}

func readAnswer(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var ans Answer
	if err := json.Unmarshal(raw, &ans); err != nil {
		return fmt.Errorf("answer unreadable: %w", err)
	}
	log.Printf("answer from the seeker's gate (asked %s):", ans.AskedAt)
	for _, k := range sortedKeys(ans.Released) {
		log.Printf("  released %-14s %s", k, ans.Released[k])
	}
	for _, k := range sortedKeys(ans.Withheld) {
		log.Printf("  withheld %-14s %s", k, ans.Withheld[k])
	}
	if len(ans.Released) == 0 {
		log.Printf("nothing released — the refusals above say exactly what would change that")
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
