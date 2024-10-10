package crdupgradesafety

import (
	"errors"
	"testing"

	kappcus "carvel.dev/kapp/pkg/kapp/crdupgradesafety"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/utils/ptr"
)

type testcase struct {
	name    string
	diff    kappcus.FieldDiff
	err     error
	handled bool
}

func TestEnum(t *testing.T) {
	for _, tc := range []testcase{
		{
			name: "no diff, no error, handled",
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
			diff: kappcus.FieldDiff{
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
