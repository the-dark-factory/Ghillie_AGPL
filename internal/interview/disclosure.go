package interview

import "strings"

// Disclosure is what ghillie says when the client asks about the machinery
// behind it. The rule, decided 2026-07-30: EXPLAIN OUTCOME, DECLINE MECHANISM.
//
// The precedent on file is "we describe methods by their results, not their
// internals", and ghillie can hold that line HONESTLY rather than evasively —
// because it genuinely does not hold the scoring. It is an interviewing agent
// fulfilling a brief; the judging happens somewhere it cannot see. Declining to
// explain a thing you do not know is not a dodge, and the register should sound
// like what it is: warm, plain, faintly amused at being credited with more than
// it has.
//
// What ghillie MAY explain in full: what the client gets, what is guaranteed,
// what it refused and why. A refusal the user can see is the asset.
// What it declines: how the factory scores a specification.

// Topic classifies what the client just asked about, when they asked about the
// machinery rather than answering the question.
type Topic uint8

// The disclosure topics ghillie recognises. NoTopic means the client said
// something ordinary and this file has no business with it.
const (
	NoTopic Topic = iota
	TopicHowItJudges
	TopicAmIDone
	TopicWhatIsThisFor
	TopicCost
)

// disclosureTriggers maps a topic to the phrases that raise it. This is
// KEYWORD ROUTING OF THE CONVERSATION, and it is worth being clear that it is
// not judgement of any kind: it decides which sentence ghillie says next, never
// what an answer is worth. Ghillie judges nothing.
var disclosureTriggers = []struct {
	topic  Topic
	phrase []string // substring match, any reply shape
	bare   []string // single common words: raised ONLY by a question-shaped reply
}{
	{TopicHowItJudges, []string{
		"how do you judge", "how does it judge", "how do you score", "how does it score",
		"how are you scoring", "how does the factory judge", "how does the factory decide",
		"what are you looking for", "how do you work out", "how does it work out",
		"what is the algorithm", "how does it grade",
	}, nil},
	{TopicAmIDone, []string{
		"is that enough", "have i said enough", "am i done", "are we done",
		"is the spec ready", "is it ready", "is that everything", "do you have enough",
		"is my spec complete", "is that good enough",
	}, nil},
	{TopicWhatIsThisFor, []string{
		"what is this for", "what are you doing with this", "who sees this",
		"where does this go", "what happens to my answers", "why are you asking",
	}, nil},
	{TopicCost, []string{
		"how much", "what will it cost", "what does it cost", "how expensive",
		"what am i paying",
	}, []string{
		// ★ BARE MONEY-WORDS ARE CONTENT, NOT QUESTIONS. "unit price in
		// pennies" is a person DESCRIBING their software, and 2026-08-03 this
		// table swallowed exactly that answer: Contains("price") matched, the
		// item was re-put, and every later answer in the sitting landed one
		// item late — the scrambled c1 submissions in the coordinator's store are
		// this defect's artifact. A spec about money cannot be described
		// without money words, so these raise the topic only when the reply
		// reads as the client ASKING (questionShaped below).
		"price", "cost",
	}},
}

// questionShaped reports whether a reply reads as the client asking something
// rather than describing something. Routing only, never judgement: a "?" is
// asking; so is a short reply that opens interrogatively (transcribed speech
// rarely carries punctuation). A long interrogative-opening reply is treated
// as an answer — "When a line is malformed, the cost is refused…" is a person
// specifying, and swallowing it is the worse mistake (the 2026-08-03 rule:
// mis-hearing an answer as a question loses the person's words; mis-hearing a
// question as an answer merely records it).
func questionShaped(s string) bool {
	if strings.HasSuffix(s, "?") {
		return true
	}
	leads := []string{
		"what ", "what's ", "how ", "why ", "who ", "where ", "when ",
		"is ", "are ", "am ", "do ", "does ", "did ", "will ", "can ",
		"could ", "would ", "should ",
	}
	for _, lead := range leads {
		if strings.HasPrefix(s, lead) {
			return len(strings.Fields(s)) <= 12
		}
	}
	return false
}

// Classify reports which disclosure topic, if any, a client's reply raises.
//
// It is deliberately conservative: an unrecognised reply is NoTopic and is
// treated as an ordinary answer, because guessing wrong in that direction only
// records what the client said, while guessing wrong in the other direction
// would have ghillie lecture someone who was answering the question.
func Classify(reply string) Topic {
	s := strings.ToLower(strings.TrimSpace(reply))
	if s == "" {
		return NoTopic
	}
	for _, t := range disclosureTriggers {
		for _, p := range t.phrase {
			if strings.Contains(s, p) {
				return t.topic
			}
		}
		for _, w := range t.bare {
			if strings.Contains(s, w) && questionShaped(s) {
				return t.topic
			}
		}
	}
	return NoTopic
}

// Answer returns what ghillie says about a topic.
//
// Every one of these strings is CLIENT-VISIBLE and is subject to the opsec pass
// before a client sees it. None of them names an internal mechanism, and none of
// them claims a judgement ghillie is not entitled to make.
func (t Topic) Answer() string {
	switch t {
	case TopicHowItJudges:
		return "Honestly? I could not tell you. I am the one sitting with you taking the notes — " +
			"the working out happens back at the factory and I do not see it. " +
			"What I can tell you is what comes out the other end: what gets built, what is guaranteed about it, " +
			"and every place the thing refused to do something and said so out loud. " +
			"We describe our methods by their results rather than their internals, and in my case that is not " +
			"a polite way of keeping a secret — I genuinely do not hold it."

	case TopicAmIDone:
		return "I cannot tell you that, and I would not trust me if I did. " +
			"Whether there is enough here is for the people who cost it and build it, not for the one asking the questions. " +
			"What I can do is show you exactly what I have got and what I have not, and let them decide."

	case TopicWhatIsThisFor:
		return "You are describing a piece of software you want built, and I am getting your description on the record " +
			"in your own words rather than mine. It goes to the factory, which works out what to build and what it costs. " +
			"I do not decide anything about it — I ask, I write down what you say, and I tell you plainly when I did not get something."

	case TopicCost:
		return CostReminder()

	default:
		return ""
	}
}

// CostReminder is the thing ghillie is REQUIRED to be able to say, and to say
// unprompted at least once: being open about cost is in character, not a
// bolt-on. The incentive alignment behind it is genuine — a precise description
// is a cheaper build — so it is not a sales line and should not sound like one.
func CostReminder() string {
	return "I will not quote you a price — that is the factory's to work out, not mine. " +
		"What I will say, because it is true and it is in your interest: the more precisely you describe this, " +
		"the better it gets built and the less it costs. Vagueness is the expensive part. Detail is the cheap part."
}

// String names a topic, for the log.
func (t Topic) String() string {
	switch t {
	case TopicHowItJudges:
		return "how-it-judges"
	case TopicAmIDone:
		return "am-i-done"
	case TopicWhatIsThisFor:
		return "what-is-this-for"
	case TopicCost:
		return "cost"
	default:
		return "none"
	}
}
