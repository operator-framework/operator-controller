package util

import (
	"context"
	"testing"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/openshift/api/platform/v1alpha1"
	platformtypes "github.com/operator-framework/operator-controller/api/v1alpha1"
)

func Test_inspectPlatformOperator(t *testing.T) {
	type args struct {
		po platformv1alpha1.PlatformOperator
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "HappyPath",
			args: args{
				po: platformv1alpha1.PlatformOperator{
					Status: platformv1alpha1.PlatformOperatorStatus{
						Conditions: []metav1.Condition{
							{
								Type:   platformtypes.TypeInstalled,
								Status: metav1.ConditionTrue,
								Reason: platformtypes.ReasonInstallSuccessful,
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "NilConditions",
			args: args{
				po: platformv1alpha1.PlatformOperator{
					Status: platformv1alpha1.PlatformOperatorStatus{
						Conditions: []metav1.Condition{},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "InstallFailed",
			args: args{
				po: platformv1alpha1.PlatformOperator{
					Status: platformv1alpha1.PlatformOperatorStatus{
						Conditions: []metav1.Condition{
							{
								Type:   platformtypes.TypeInstalled,
								Status: metav1.ConditionFalse,
								Reason: platformtypes.ReasonInstallFailed,
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "SourceFailed",
			args: args{
				po: platformv1alpha1.PlatformOperator{
					Status: platformv1alpha1.PlatformOperatorStatus{
						Conditions: []metav1.Condition{
							{
								Type:   platformtypes.TypeInstalled,
								Status: metav1.ConditionFalse,
								Reason: platformtypes.ReasonSourceFailed,
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "MissingAppliedCondition",
			args: args{
				po: platformv1alpha1.PlatformOperator{
					Status: platformv1alpha1.PlatformOperatorStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "stub",
								Status: metav1.ConditionTrue,
								Reason: platformtypes.ReasonSourceFailed,
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := inspectPlatformOperator(tt.args.po); (err != nil) != tt.wantErr {
				t.Errorf("inspectPlatformOperator() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInspectBundleDeployment(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		want       *metav1.Condition
	}{
		{
			name: "InstallSucceeded",
			conditions: []metav1.Condition{
				{
					Type:   rukpakv1alpha1.TypeHasValidBundle,
					Status: metav1.ConditionTrue,
					Reason: rukpakv1alpha1.ReasonUnpackSuccessful,
				},
				{
					Type:   rukpakv1alpha1.TypeInstalled,
					Status: metav1.ConditionTrue,
					Reason: rukpakv1alpha1.ReasonInstallationSucceeded,
				},
			},
			want: nil,
		},
		{
			name: "InstalledWrongReason",
			conditions: []metav1.Condition{
				{
					Type:   rukpakv1alpha1.TypeHasValidBundle,
					Status: metav1.ConditionTrue,
					Reason: rukpakv1alpha1.ReasonUnpackSuccessful,
				},
				{
					Type:   rukpakv1alpha1.TypeInstalled,
					Status: metav1.ConditionTrue,
					Reason: rukpakv1alpha1.ReasonErrorGettingClient,
				},
			},
			want: nil,
		},
		{
			name: "InstallFailed",
			conditions: []metav1.Condition{
				{
					Type:   rukpakv1alpha1.TypeHasValidBundle,
					Status: metav1.ConditionTrue,
					Reason: rukpakv1alpha1.ReasonUnpackSuccessful,
				},
				{
					Type:   rukpakv1alpha1.TypeInstalled,
					Status: metav1.ConditionFalse,
					Reason: rukpakv1alpha1.ReasonInstallFailed,
				},
			},
			want: &metav1.Condition{
				Type:   platformtypes.TypeInstalled,
				Status: metav1.ConditionFalse,
				Reason: rukpakv1alpha1.ReasonInstallFailed,
			},
		},
		{
			name: "UnpackedButNotInstalled",
			conditions: []metav1.Condition{
				{
					Type:   rukpakv1alpha1.TypeHasValidBundle,
					Status: metav1.ConditionTrue,
					Reason: rukpakv1alpha1.ReasonUnpackSuccessful,
				},
				{
					Type:   "stub",
					Status: metav1.ConditionFalse,
					Reason: rukpakv1alpha1.ReasonInstallFailed,
				},
			},
			want: &metav1.Condition{
				Type:   platformtypes.TypeInstalled,
				Status: metav1.ConditionFalse,
				Reason: platformtypes.ReasonInstallPending,
			},
		},
		{
			name: "UnpackedNil",
			conditions: []metav1.Condition{
				{
					Type:   "stub",
					Status: metav1.ConditionFalse,
					Reason: rukpakv1alpha1.ReasonInstallFailed,
				},
			},
			want: &metav1.Condition{
				Type:    platformtypes.TypeInstalled,
				Status:  metav1.ConditionFalse,
				Reason:  platformtypes.ReasonUnpackPending,
				Message: "Waiting for the bundle to be unpacked",
			},
		},
		{
			name: "UnpackFailed",
			conditions: []metav1.Condition{
				{
					Type:   rukpakv1alpha1.TypeHasValidBundle,
					Status: metav1.ConditionFalse,
					Reason: rukpakv1alpha1.ReasonUnpackPending,
				},
			},
			want: &metav1.Condition{
				Type:   platformtypes.TypeInstalled,
				Status: metav1.ConditionFalse,
				Reason: rukpakv1alpha1.ReasonUnpackPending,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InspectBundleDeployment(context.Background(), tt.conditions); !conditionsAreEqual(got, tt.want) {
				t.Errorf("name = %s, InspectBundleDeployment() = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func conditionsAreEqual(a, b *metav1.Condition) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil && b != nil {
		return false
	}
	if a != nil && b == nil {
		return false
	}
	return a.Type == b.Type && a.Status == b.Status && a.Reason == b.Reason
}
