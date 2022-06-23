package solver

// Identifier values uniquely identify particular Variables within
// the input to a single call to Solve.
type Identifier string

func (id Identifier) String() string {
	return string(id)
}

// IdentifierFromString returns an Identifier based on a provided
// string.
func IdentifierFromString(s string) Identifier {
	return Identifier(s)
}

// Variable values are the basic unit of problems and solutions
// understood by this package.
type Variable interface {
	// Identifier returns the Identifier that uniquely identifies
	// this Variable among all other Variables in a given
	// problem.
	Identifier() Identifier
	// Constraints returns the set of constraints that apply to
	// this Variable.
	Constraints() []Constraint
}

// zeroVariable is returned by VariableOf in error cases.
type zeroVariable struct{}

var _ Variable = zeroVariable{}

func (zeroVariable) Identifier() Identifier {
	return ""
}

func (zeroVariable) Constraints() []Constraint {
	return nil
}

type GenericVariable struct {
	ID    Identifier
	Rules []Constraint
}

func (i GenericVariable) Identifier() Identifier {
	return i.ID
}

func (i GenericVariable) Constraints() []Constraint {
	return i.Rules
}

func NewVariable(id Identifier, constraints ...Constraint) Variable {
	return GenericVariable{
		ID:    id,
		Rules: constraints,
	}
}

func PrettyConstraint(c Constraint, msg string) Constraint {
	return prettyConstraint{
		Constraint: c,
		msg:        msg,
	}
}

type prettyConstraint struct {
	Constraint
	msg string
}

func (pc prettyConstraint) String(_ Identifier) string {
	return pc.msg
}
