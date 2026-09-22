package jev

// A rule is one statement put to the model about one subject. Every statement is phrased so
// that the defect reads high: a large probability means the same thing under every rule.
//
// ponytail: every cutoff is the provisional 0.5 until `MEKIKI_JEV_LIVE=1 go test -run Live`
// has recorded a baseline on the fixtures under testdata/. Move one only into the gap
// between the highest clean and the lowest bad answer.
type rule struct {
	ID        string
	Statement string
	WhenTrue  string
	WhenFalse string
	Cutoff    float64
}

var rules = map[string]rule{
	"J1": {
		ID: "J1",
		Statement: "The sentence under `sentence` describes a computation, threshold comparison or " +
			"data transformation that a script could perform deterministically, rather than a judgement " +
			"the reader has to make.",
		WhenTrue: "The sentence tells the reader to compute, divide, sum, count, compare against a " +
			"number, or transform data by a fixed procedure whose result does not depend on judgement.",
		WhenFalse: "The sentence explains a criterion, a reason, a trade-off, or a decision that needs " +
			"context a script does not have; or it merely names a script, a file or a tool.",
		Cutoff: 0.5,
	},
	"J2": {
		ID: "J2",
		Statement: "The Contract's Trigger states an activation condition that contradicts, or is " +
			"unrelated to, the description.",
		WhenTrue: "The Trigger names a different situation, user request or subject than the " +
			"description does, so a reader of one would not predict the other.",
		WhenFalse: "The Trigger restates, narrows or gives an example of the situation the description " +
			"describes, even in different words.",
		Cutoff: 0.5,
	},
	"J3": {
		ID: "J3",
		Statement: "The description omits at least one of: what the skill does, when to use it, or " +
			"which neighbouring skill or stage to use instead.",
		WhenTrue: "One of the three is missing: there is no statement of what the skill does, or no " +
			"situation or request wording that says when to use it, or no redirect naming what to use " +
			"when this skill does not apply.",
		WhenFalse: "All three are present in some form: what it does, a concrete situation or request " +
			"that activates it, and a named alternative for when it does not apply.",
		Cutoff: 0.5,
	},
	"J4": {
		ID: "J4",
		Statement: "Judging from the lines under `sentences`, this skill itself performs the operation " +
			"named under `operation` as one of its steps, rather than mentioning, warning about, or " +
			"delegating it.",
		WhenTrue: "A step of this skill carries out the operation: it publishes, sends, posts, " +
			"deletes, overwrites or otherwise executes it, possibly after a confirmation.",
		WhenFalse: "The skill only mentions the operation: it warns against it, describes another " +
			"skill or a person doing it, lists it as out of scope, or names a guard for it.",
		Cutoff: 0.5,
	},
	"J5": {
		ID: "J5",
		Statement: "In the entry under `non_goals`, at least one excluded piece of work is not followed " +
			"by the name of the skill, stage, person or tool that handles it instead.",
		WhenTrue: "Some excluded item stands alone: nothing after it says who or what does that work. " +
			"A bare \"does not X; does not Y\" with no names is the typical case.",
		WhenFalse: "Every excluded item is paired with a handler: a skill name (hyphenated, like " +
			"reviewing-schedule-delta, in parentheses or in prose), a stage, a person or a tool; or the " +
			"entry states that nobody does that work at all.",
		Cutoff: 0.5,
	},
}
