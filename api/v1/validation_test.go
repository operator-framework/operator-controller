package v1

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestValidate(t *testing.T) {
	type args struct {
		object         any
		skipDefaulting bool
	}
	type want struct {
		valid bool
	}
	type testCase struct {
		args args
		want want
	}
	defaultExtensionSpec := func(s *ClusterExtensionSpec) *ClusterExtensionSpec {
		s.Namespace = "ns"
		s.ServiceAccount = ServiceAccountReference{
			Name: "sa",
		}
		s.Source = SourceConfig{
			SourceType: SourceTypeCatalog,
			Catalog: &CatalogFilter{
				PackageName: "test",
			},
		}
		return s
	}
	defaultRevisionSpec := func(s *ClusterExtensionRevisionSpec) *ClusterExtensionRevisionSpec {
		s.Revision = 1
		return s
	}
	c := newClient(t)
	i := 0

	for name, tc := range map[string]testCase{
		"ClusterExtension: invalid progress deadline < 10": {
			args: args{
				object: ClusterExtensionSpec{
					ProgressDeadlineMinutes: 9,
				},
			},
			want: want{valid: false},
		},
		"ClusterExtension: valid progress deadline = 10": {
			args: args{
				object: ClusterExtensionSpec{
					ProgressDeadlineMinutes: 10,
				},
			},
			want: want{valid: true},
		},
		"ClusterExtension: valid progress deadline = 360": {
			args: args{
				object: ClusterExtensionSpec{
					ProgressDeadlineMinutes: 360,
				},
			},
			want: want{valid: true},
		},
		"ClusterExtension: valid progress deadline = 720": {
			args: args{
				object: ClusterExtensionSpec{
					ProgressDeadlineMinutes: 720,
				},
			},
			want: want{valid: true},
		},
		"ClusterExtension: invalid progress deadline > 720": {
			args: args{
				object: ClusterExtensionSpec{
					ProgressDeadlineMinutes: 721,
				},
			},
			want: want{valid: false},
		},
		"ClusterExtension: no progress deadline set": {
			args: args{
				object: ClusterExtensionSpec{},
			},
			want: want{valid: true},
		},
		"ClusterExtensionRevision: invalid progress deadline < 10": {
			args: args{
				object: ClusterExtensionRevisionSpec{
					ProgressDeadlineMinutes: 9,
				},
			},
			want: want{valid: false},
		},
		"ClusterExtensionRevision: valid progress deadline = 10": {
			args: args{
				object: ClusterExtensionRevisionSpec{
					ProgressDeadlineMinutes: 10,
				},
			},
			want: want{valid: true},
		},
		"ClusterExtensionRevision: valid progress deadline = 360": {
			args: args{
				object: ClusterExtensionRevisionSpec{
					ProgressDeadlineMinutes: 360,
				},
			},
			want: want{valid: true},
		},
		"ClusterExtensionRevision: valid progress deadline = 720": {
			args: args{
				object: ClusterExtensionRevisionSpec{
					ProgressDeadlineMinutes: 720,
				},
			},
			want: want{valid: true},
		},
		"ClusterExtensionRevision: invalid progress deadline > 720": {
			args: args{
				object: ClusterExtensionRevisionSpec{
					ProgressDeadlineMinutes: 721,
				},
			},
			want: want{valid: false},
		},
		"ClusterExtensionRevision: no progress deadline set": {
			args: args{
				object: ClusterExtensionRevisionSpec{},
			},
			want: want{valid: true},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var obj client.Object
			switch s := tc.args.object.(type) {
			case ClusterExtensionSpec:
				ce := &ClusterExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("ce-%d", i),
					},
					Spec: s,
				}
				if !tc.args.skipDefaulting {
					defaultExtensionSpec(&ce.Spec)
				}
				obj = ce
			case ClusterExtensionRevisionSpec:
				cer := &ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("cer-%d", i),
					},
					Spec: s,
				}
				if !tc.args.skipDefaulting {
					defaultRevisionSpec(&cer.Spec)
				}
				obj = cer
			default:
				t.Fatalf("unknown type %T", s)
			}
			i++
			err := c.Create(t.Context(), obj)
			if tc.want.valid && err != nil {
				t.Fatal("expected create to succeed, but got:", err)
			}
			if !tc.want.valid && !errors.IsInvalid(err) {
				t.Fatal("expected create to fail due to invalid payload, but got:", err)
			}
		})
	}
}
