package util_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/rukpak/util"
)

func TestDeepHashObject(t *testing.T) {
	tests := []struct {
		name         string
		wantErr      bool
		obj          interface{}
		expectedHash string
	}{
		{
			name:    "populated obj with exported fields",
			wantErr: false,
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
			expectedHash: "3ZuoKXfN7Bu",
		},
		{
			name:    "modified populated obj with exported fields",
			wantErr: false,
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
			expectedHash: "hhbF1vhCexh",
		},
		{
			name:    "populated obj with unexported fields",
			wantErr: false,
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
			expectedHash: "h6o53zkh2pL",
		},
		{
			// The JSON encoder requires exported fields or it will generate
			// the same hash as a completely empty object
			name:         "empty obj",
			wantErr:      false,
			obj:          struct{}{},
			expectedHash: "h6o53zkh2pL",
		},
		{
			name:         "string a",
			wantErr:      false,
			obj:          "a",
			expectedHash: "60DgQGu2X8Q",
		},
		{
			name:         "string b",
			wantErr:      false,
			obj:          "b",
			expectedHash: "8ar1VDrlnS5",
		},
		{
			name:         "nil obj",
			wantErr:      false,
			obj:          nil,
			expectedHash: "3FovGixHSl4",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := util.DeepHashObject(tc.obj)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedHash, hash)
			}
		})
	}
}
