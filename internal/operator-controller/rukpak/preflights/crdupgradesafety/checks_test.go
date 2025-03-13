package crdupgradesafety

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/utils/ptr"
)

type testcase struct {
	name    string
	diff    FieldDiff
	err     error
	handled bool
}

func TestEnum(t *testing.T) {
	for _, tc := range []testcase{
		{
			name: "no diff, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Enum: []apiextensionsv1.JSON{
						{
							Raw: []byte("foo"),
						},
					},
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Enum: []apiextensionsv1.JSON{
						{
							Raw: []byte("foo"),
						},
					},
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "new enum constraint, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Enum: []apiextensionsv1.JSON{},
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Enum: []apiextensionsv1.JSON{
						{
							Raw: []byte("foo"),
						},
					},
				},
			},
			err:     errors.New("enum constraints [foo] added when there were no restrictions previously"),
			handled: true,
		},
		{
			name: "remove enum value, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Enum: []apiextensionsv1.JSON{
						{
							Raw: []byte("foo"),
						},
						{
							Raw: []byte("bar"),
						},
					},
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Enum: []apiextensionsv1.JSON{
						{
							Raw: []byte("bar"),
						},
					},
				},
			},
			err:     errors.New("enums [foo] removed from the set of previously allowed values"),
			handled: true,
		},
		{
			name: "different field changed, no error, not handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					ID: "foo",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					ID: "bar",
				},
			},
			err:     nil,
			handled: false,
		},
		{
			name: "different field changed with enum, no error, not handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					ID: "foo",
					Enum: []apiextensionsv1.JSON{
						{
							Raw: []byte("foo"),
						},
					},
				},
				New: &apiextensionsv1.JSONSchemaProps{
					ID: "bar",
					Enum: []apiextensionsv1.JSON{
						{
							Raw: []byte("foo"),
						},
					},
				},
			},
			err:     nil,
			handled: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := Enum(tc.diff)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.handled, handled)
		})
	}
}

func TestRequired(t *testing.T) {
	for _, tc := range []testcase{
		{
			name: "no diff, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Required: []string{
						"foo",
					},
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Required: []string{
						"foo",
					},
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "new required field, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{},
				New: &apiextensionsv1.JSONSchemaProps{
					Required: []string{
						"foo",
					},
				},
			},
			err:     errors.New("new required fields [foo] added"),
			handled: true,
		},
		{
			name: "different field changed, no error, not handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					ID: "foo",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					ID: "bar",
				},
			},
			err:     nil,
			handled: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := Required(tc.diff)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.handled, handled)
		})
	}
}

func TestMaximum(t *testing.T) {
	for _, tc := range []testcase{
		{
			name: "no diff, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Maximum: ptr.To(10.0),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Maximum: ptr.To(10.0),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "new maximum constraint, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{},
				New: &apiextensionsv1.JSONSchemaProps{
					Maximum: ptr.To(10.0),
				},
			},
			err:     errors.New("maximum: constraint 10 added when there were no restrictions previously"),
			handled: true,
		},
		{
			name: "maximum constraint decreased, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Maximum: ptr.To(20.0),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Maximum: ptr.To(10.0),
				},
			},
			err:     errors.New("maximum: constraint decreased from 20 to 10"),
			handled: true,
		},
		{
			name: "maximum constraint increased, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Maximum: ptr.To(20.0),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Maximum: ptr.To(30.0),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "different field changed, no error, not handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					ID: "foo",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					ID: "bar",
				},
			},
			err:     nil,
			handled: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := Maximum(tc.diff)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.handled, handled)
		})
	}
}

func TestMaxItems(t *testing.T) {
	for _, tc := range []testcase{
		{
			name: "no diff, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MaxItems: ptr.To(int64(10)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MaxItems: ptr.To(int64(10)),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "new maxItems constraint, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{},
				New: &apiextensionsv1.JSONSchemaProps{
					MaxItems: ptr.To(int64(10)),
				},
			},
			err:     errors.New("maxItems: constraint 10 added when there were no restrictions previously"),
			handled: true,
		},
		{
			name: "maxItems constraint decreased, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MaxItems: ptr.To(int64(20)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MaxItems: ptr.To(int64(10)),
				},
			},
			err:     errors.New("maxItems: constraint decreased from 20 to 10"),
			handled: true,
		},
		{
			name: "maxitems constraint increased, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MaxItems: ptr.To(int64(10)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MaxItems: ptr.To(int64(20)),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "different field changed, no error, not handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					ID: "foo",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					ID: "bar",
				},
			},
			err:     nil,
			handled: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := MaxItems(tc.diff)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.handled, handled)
		})
	}
}

func TestMaxLength(t *testing.T) {
	for _, tc := range []testcase{
		{
			name: "no diff, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MaxLength: ptr.To(int64(10)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MaxLength: ptr.To(int64(10)),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "new maxLength constraint, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{},
				New: &apiextensionsv1.JSONSchemaProps{
					MaxLength: ptr.To(int64(10)),
				},
			},
			err:     errors.New("maxLength: constraint 10 added when there were no restrictions previously"),
			handled: true,
		},
		{
			name: "maxLength constraint decreased, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MaxLength: ptr.To(int64(20)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MaxLength: ptr.To(int64(10)),
				},
			},
			err:     errors.New("maxLength: constraint decreased from 20 to 10"),
			handled: true,
		},
		{
			name: "maxLength constraint increased, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MaxLength: ptr.To(int64(10)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MaxLength: ptr.To(int64(20)),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "different field changed, no error, not handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					ID: "foo",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					ID: "bar",
				},
			},
			err:     nil,
			handled: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := MaxLength(tc.diff)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.handled, handled)
		})
	}
}

func TestMaxProperties(t *testing.T) {
	for _, tc := range []testcase{
		{
			name: "no diff, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MaxProperties: ptr.To(int64(10)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MaxProperties: ptr.To(int64(10)),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "new maxProperties constraint, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{},
				New: &apiextensionsv1.JSONSchemaProps{
					MaxProperties: ptr.To(int64(10)),
				},
			},
			err:     errors.New("maxProperties: constraint 10 added when there were no restrictions previously"),
			handled: true,
		},
		{
			name: "maxProperties constraint decreased, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MaxProperties: ptr.To(int64(20)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MaxProperties: ptr.To(int64(10)),
				},
			},
			err:     errors.New("maxProperties: constraint decreased from 20 to 10"),
			handled: true,
		},
		{
			name: "maxProperties constraint increased, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MaxProperties: ptr.To(int64(10)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MaxProperties: ptr.To(int64(20)),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "different field changed, no error, not handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					ID: "foo",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					ID: "bar",
				},
			},
			err:     nil,
			handled: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := MaxProperties(tc.diff)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.handled, handled)
		})
	}
}

func TestMinItems(t *testing.T) {
	for _, tc := range []testcase{
		{
			name: "no diff, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MinItems: ptr.To(int64(10)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MinItems: ptr.To(int64(10)),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "new minItems constraint, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{},
				New: &apiextensionsv1.JSONSchemaProps{
					MinItems: ptr.To(int64(10)),
				},
			},
			err:     errors.New("minItems: constraint 10 added when there were no restrictions previously"),
			handled: true,
		},
		{
			name: "minItems constraint decreased, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MinItems: ptr.To(int64(20)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MinItems: ptr.To(int64(10)),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "minItems constraint increased, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MinItems: ptr.To(int64(10)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MinItems: ptr.To(int64(20)),
				},
			},
			err:     errors.New("minItems: constraint increased from 10 to 20"),
			handled: true,
		},
		{
			name: "different field changed, no error, not handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					ID: "foo",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					ID: "bar",
				},
			},
			err:     nil,
			handled: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := MinItems(tc.diff)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.handled, handled)
		})
	}
}

func TestMinimum(t *testing.T) {
	for _, tc := range []testcase{
		{
			name: "no diff, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Minimum: ptr.To(10.0),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Minimum: ptr.To(10.0),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "new minimum constraint, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{},
				New: &apiextensionsv1.JSONSchemaProps{
					Minimum: ptr.To(10.0),
				},
			},
			err:     errors.New("minimum: constraint 10 added when there were no restrictions previously"),
			handled: true,
		},
		{
			name: "minLength constraint decreased, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Minimum: ptr.To(20.0),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Minimum: ptr.To(10.0),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "minLength constraint increased, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Minimum: ptr.To(10.0),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Minimum: ptr.To(20.0),
				},
			},
			err:     errors.New("minimum: constraint increased from 10 to 20"),
			handled: true,
		},
		{
			name: "different field changed, no error, not handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					ID: "foo",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					ID: "bar",
				},
			},
			err:     nil,
			handled: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := Minimum(tc.diff)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.handled, handled)
		})
	}
}

func TestMinLength(t *testing.T) {
	for _, tc := range []testcase{
		{
			name: "no diff, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MinLength: ptr.To(int64(10)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MinLength: ptr.To(int64(10)),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "new minLength constraint, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{},
				New: &apiextensionsv1.JSONSchemaProps{
					MinLength: ptr.To(int64(10)),
				},
			},
			err:     errors.New("minLength: constraint 10 added when there were no restrictions previously"),
			handled: true,
		},
		{
			name: "minLength constraint decreased, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MinLength: ptr.To(int64(20)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MinLength: ptr.To(int64(10)),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "minLength constraint increased, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MinLength: ptr.To(int64(10)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MinLength: ptr.To(int64(20)),
				},
			},
			err:     errors.New("minLength: constraint increased from 10 to 20"),
			handled: true,
		},
		{
			name: "different field changed, no error, not handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					ID: "foo",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					ID: "bar",
				},
			},
			err:     nil,
			handled: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := MinLength(tc.diff)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.handled, handled)
		})
	}
}

func TestMinProperties(t *testing.T) {
	for _, tc := range []testcase{
		{
			name: "no diff, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MinProperties: ptr.To(int64(10)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MinProperties: ptr.To(int64(10)),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "new minProperties constraint, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{},
				New: &apiextensionsv1.JSONSchemaProps{
					MinProperties: ptr.To(int64(10)),
				},
			},
			err:     errors.New("minProperties: constraint 10 added when there were no restrictions previously"),
			handled: true,
		},
		{
			name: "minProperties constraint decreased, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MinProperties: ptr.To(int64(20)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MinProperties: ptr.To(int64(10)),
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "minProperties constraint increased, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					MinProperties: ptr.To(int64(10)),
				},
				New: &apiextensionsv1.JSONSchemaProps{
					MinProperties: ptr.To(int64(20)),
				},
			},
			err:     errors.New("minProperties: constraint increased from 10 to 20"),
			handled: true,
		},
		{
			name: "different field changed, no error, not handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					ID: "foo",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					ID: "bar",
				},
			},
			err:     nil,
			handled: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := MinProperties(tc.diff)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.handled, handled)
		})
	}
}

func TestDefault(t *testing.T) {
	for _, tc := range []testcase{
		{
			name: "no diff, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Default: &apiextensionsv1.JSON{
						Raw: []byte("foo"),
					},
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Default: &apiextensionsv1.JSON{
						Raw: []byte("foo"),
					},
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "new default value, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{},
				New: &apiextensionsv1.JSONSchemaProps{
					Default: &apiextensionsv1.JSON{
						Raw: []byte("foo"),
					},
				},
			},
			err:     errors.New("default value \"foo\" added when there was no default previously"),
			handled: true,
		},
		{
			name: "default value removed, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Default: &apiextensionsv1.JSON{
						Raw: []byte("foo"),
					},
				},
				New: &apiextensionsv1.JSONSchemaProps{},
			},
			err:     errors.New("default value \"foo\" removed"),
			handled: true,
		},
		{
			name: "default value changed, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Default: &apiextensionsv1.JSON{
						Raw: []byte("foo"),
					},
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Default: &apiextensionsv1.JSON{
						Raw: []byte("bar"),
					},
				},
			},
			err:     errors.New("default value changed from \"foo\" to \"bar\""),
			handled: true,
		},
		{
			name: "different field changed, no error, not handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					ID: "foo",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					ID: "bar",
				},
			},
			err:     nil,
			handled: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := Default(tc.diff)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.handled, handled)
		})
	}
}

func TestType(t *testing.T) {
	for _, tc := range []testcase{
		{
			name: "no diff, no error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Type: "string",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Type: "string",
				},
			},
			err:     nil,
			handled: true,
		},
		{
			name: "type changed, error, handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					Type: "string",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					Type: "integer",
				},
			},
			err:     errors.New("type changed from \"string\" to \"integer\""),
			handled: true,
		},
		{
			name: "different field changed, no error, not handled",
			diff: FieldDiff{
				Old: &apiextensionsv1.JSONSchemaProps{
					ID: "foo",
				},
				New: &apiextensionsv1.JSONSchemaProps{
					ID: "bar",
				},
			},
			err:     nil,
			handled: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handled, err := Type(tc.diff)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.handled, handled)
		})
	}
}

func TestOrderKappsValidateErr(t *testing.T) {
	testErr1 := errors.New("fallback1")
	testErr2 := errors.New("fallback2")

	generateErrors := func(n int, base string) []error {
		var result []error
		for i := n; i >= 0; i-- {
			result = append(result, fmt.Errorf("%s%d", base, i))
		}
		return result
	}

	joinedAndNested := func(format string, errs ...error) error {
		return fmt.Errorf(format, errors.Join(errs...))
	}

	testCases := []struct {
		name          string
		inpuError     error
		expectedError error
	}{
		{
			name:          "fallback: initial error was not error.Join'ed",
			inpuError:     testErr1,
			expectedError: testErr1,
		},
		{
			name:          "fallback: nested error was not wrapped",
			inpuError:     errors.Join(testErr1),
			expectedError: testErr1,
		},
		{
			name:          "fallback: multiple nested errors, one was not wrapped",
			inpuError:     errors.Join(testErr2, fmt.Errorf("%w", testErr1)),
			expectedError: errors.Join(testErr2, fmt.Errorf("%w", testErr1)),
		},
		{
			name:          "fallback: nested error did not contain \":\"",
			inpuError:     errors.Join(fmt.Errorf("%w", testErr1)),
			expectedError: testErr1,
		},
		{
			name:          "fallback: multiple nested errors, one did not contain \":\"",
			inpuError:     errors.Join(joinedAndNested("fail: %w", testErr2), joinedAndNested("%w", testErr1)),
			expectedError: errors.Join(fmt.Errorf("fail: %w", testErr2), testErr1),
		},
		{
			name:          "fallback: nested error was not error.Join'ed",
			inpuError:     errors.Join(fmt.Errorf("fail: %w", testErr1)),
			expectedError: fmt.Errorf("fail: %w", testErr1),
		},
		{
			name:          "fallback: multiple nested errors, one was not error.Join'ed",
			inpuError:     errors.Join(joinedAndNested("fail: %w", testErr2), fmt.Errorf("fail: %w", testErr1)),
			expectedError: fmt.Errorf("fail: %w\nfail: %w", testErr2, testErr1),
		},
		{
			name:          "ensures order for a single group of multiple deeply nested errors",
			inpuError:     errors.Join(joinedAndNested("fail: %w", testErr2, testErr1)),
			expectedError: fmt.Errorf("fail: %w\n%w", testErr1, testErr2),
		},
		{
			name: "ensures order for multiple groups of deeply nested errors",
			inpuError: errors.Join(
				joinedAndNested("fail: %w", testErr2, testErr1),
				joinedAndNested("validation: %w", generateErrors(5, "err")...),
			),
			expectedError: fmt.Errorf("fail: %w\n%w\nvalidation: err0\nerr1\nerr2\nerr3\nerr4\nerr5", testErr1, testErr2),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := orderKappsValidateErr(tc.inpuError)
			require.EqualError(t, err, tc.expectedError.Error())
		})
	}
}

func TestServedVersionValidator(t *testing.T) {
	validationErr1 := errors.New(`version "v1alpha1", field "^" has unknown change, refusing to determine that change is safe`)
	validationErr2 := errors.New(`version upgrade "v1alpha1" to "v1alpha2", field "^": fail`)

	for _, tc := range []struct {
		name                   string
		servedVersionValidator *ServedVersionValidator
		new                    apiextensionsv1.CustomResourceDefinition
		expectedError          error
	}{
		{
			name: "no changes, no error",
			servedVersionValidator: &ServedVersionValidator{
				Validations: []ChangeValidation{
					func(_ FieldDiff) (bool, error) {
						return false, errors.New("should not run")
					},
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name:   "v1alpha1",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{},
							},
						},
					},
				},
			},
		},
		{
			name: "changes, validation successful, change is fully handled, no error",
			servedVersionValidator: &ServedVersionValidator{
				Validations: []ChangeValidation{
					func(_ FieldDiff) (bool, error) {
						return true, nil
					},
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name:   "v1alpha1",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{},
							},
						},
						{
							Name:   "v1alpha2",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									ID: "foo",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "changes, validation successful, change not fully handled, error",
			servedVersionValidator: &ServedVersionValidator{
				Validations: []ChangeValidation{
					func(_ FieldDiff) (bool, error) {
						return false, nil
					},
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name:   "v1alpha1",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{},
							},
						},
						{
							Name:   "v1alpha2",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									ID: "foo",
								},
							},
						},
					},
				},
			},
			expectedError: validationErr1,
		},
		{
			name: "changes, validation failed, change fully handled, error",
			servedVersionValidator: &ServedVersionValidator{
				Validations: []ChangeValidation{
					func(_ FieldDiff) (bool, error) {
						return true, errors.New("fail")
					},
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name:   "v1alpha1",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{},
							},
						},
						{
							Name:   "v1alpha2",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									ID: "foo",
								},
							},
						},
					},
				},
			},
			expectedError: validationErr2,
		},
		{
			name: "changes, validation failed, change not fully handled, ordered error",
			servedVersionValidator: &ServedVersionValidator{
				Validations: []ChangeValidation{
					func(_ FieldDiff) (bool, error) {
						return false, errors.New("fail")
					},
					func(_ FieldDiff) (bool, error) {
						return false, errors.New("error")
					},
				},
			},
			new: apiextensionsv1.CustomResourceDefinition{
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
						{
							Name:   "v1alpha1",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{},
							},
						},
						{
							Name:   "v1alpha2",
							Served: true,
							Schema: &apiextensionsv1.CustomResourceValidation{
								OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
									ID: "foo",
								},
							},
						},
					},
				},
			},
			expectedError: fmt.Errorf("%w\n%s\n%w", validationErr2, `version upgrade "v1alpha1" to "v1alpha2", field "^": error`, validationErr1),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.servedVersionValidator.Validate(apiextensionsv1.CustomResourceDefinition{}, tc.new)
			if tc.expectedError != nil {
				assert.EqualError(t, err, tc.expectedError.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
