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
			name:    "populated obj",
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
			expectedHash: "16jfjhihxbzhfhs1k5mimq740kvioi98pfbea9q6qtf9",
		},
		{
			// An empty object generates the same hash as the populated object above... that feels wrong?
			name:         "empty obj",
			wantErr:      false,
			obj:          struct{}{},
			expectedHash: "16jfjhihxbzhfhs1k5mimq740kvioi98pfbea9q6qtf9",
		},
		{
			name:         "string a",
			wantErr:      false,
			obj:          "a",
			expectedHash: "1lu1qv1451mq7gv9upu1cx8ffffi07rel5xvbvvc44dh",
		},
		{
			name:         "string b",
			wantErr:      false,
			obj:          "b",
			expectedHash: "1ija85ah4gd0beltpfhszipkxfyqqxhp94tf2mjfgq61",
		},
		{
			name:         "nil obj",
			wantErr:      false,
			obj:          nil,
			expectedHash: "2im0kl1kwvzn46sr4cdtkvmdzrlurvj51xdzhwdht8l0",
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
