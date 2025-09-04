package hash_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hashutil "github.com/operator-framework/operator-controller/internal/shared/util/hash"
)

type unmarshalable struct{}

func (u *unmarshalable) MarshalJSON() ([]byte, error) {
	return nil, errors.New("unmarshalable")
}

func TestDeepHashObject(t *testing.T) {
	tests := []struct {
		name         string
		wantPanic    bool
		obj          interface{}
		expectedHash string
	}{
		{
			name: "populated obj with exported fields",
			obj: struct {
				Str string
				Num int
				Obj interface{}
				Arr []int
				B   bool
				N   interface{}
			}{
				Str: "foobar",
				Num: 22,
				Obj: struct{ Foo string }{Foo: "bar"},
				Arr: []int{0, 1},
				B:   true,
				N:   nil,
			},
			expectedHash: "gta1bt5sybll5qjqxdiekmjm7py93glrinmnrfb31fj",
		},
		{
			name: "modified populated obj with exported fields",
			obj: struct {
				Str string
				Num int
				Obj interface{}
				Arr []int
				B   bool
				N   interface{}
			}{
				Str: "foobar",
				Num: 23, // changed from 22 above
				Obj: struct{ Foo string }{Foo: "bar"},
				Arr: []int{0, 1},
				B:   true,
				N:   nil,
			},
			expectedHash: "1ftn1z2ieih8hsmi2a8c6mkoef6uodrtn4wtt1qapioh",
		},
		{
			name: "populated obj with unexported fields",
			obj: struct {
				str string
				num int
				obj interface{}
				arr []int
				b   bool
				n   interface{}
			}{
				str: "foobar",
				num: 22,
				obj: struct{ foo string }{foo: "bar"},
				arr: []int{0, 1},
				b:   true,
				n:   nil,
			},
			expectedHash: "16jfjhihxbzhfhs1k5mimq740kvioi98pfbea9q6qtf9",
		},
		{
			// The JSON encoder requires exported fields or it will generate
			// the same hash as a completely empty object
			name:         "empty obj",
			obj:          struct{}{},
			expectedHash: "16jfjhihxbzhfhs1k5mimq740kvioi98pfbea9q6qtf9",
		},
		{
			name:         "string a",
			obj:          "a",
			expectedHash: "1lu1qv1451mq7gv9upu1cx8ffffi07rel5xvbvvc44dh",
		},
		{
			name:         "string b",
			obj:          "b",
			expectedHash: "1ija85ah4gd0beltpfhszipkxfyqqxhp94tf2mjfgq61",
		},
		{
			name:         "nil obj",
			obj:          nil,
			expectedHash: "2im0kl1kwvzn46sr4cdtkvmdzrlurvj51xdzhwdht8l0",
		},
		{
			name:      "unmarshalable obj",
			obj:       &unmarshalable{},
			wantPanic: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			test := func() {
				hash := hashutil.DeepHashObject(tc.obj)
				assert.Equal(t, tc.expectedHash, hash)
			}

			if tc.wantPanic {
				require.Panics(t, test)
			} else {
				require.NotPanics(t, test)
			}
		})
	}
}
