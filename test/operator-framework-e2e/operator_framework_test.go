package operatore2e

import (
	"context"
	"os"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

func TestOperatorFramework(t *testing.T) {
	t.Parallel()
	cfg := ctrl.GetConfigOrDie()

	scheme := runtime.NewScheme()

	require.NoError(t, catalogd.AddToScheme(scheme))
	require.NoError(t, operatorv1alpha1.AddToScheme(scheme))

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	var operators = []*operatorv1alpha1.Operator{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "plainv0",
			},
			Spec: operatorv1alpha1.OperatorSpec{
				PackageName: os.Getenv("PLAIN_PKG_NAME"),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "registryv1",
			},
			Spec: operatorv1alpha1.OperatorSpec{
				PackageName: os.Getenv("REG_PKG_NAME"),
			},
		},
	}

	for _, op := range operators {
		operator := op
		t.Run(operator.ObjectMeta.Name, func(t *testing.T) {
			t.Parallel()
			catalog := &catalogd.Catalog{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "catalog",
				},
				Spec: catalogd.CatalogSpec{
					Source: catalogd.CatalogSource{
						Type: catalogd.SourceTypeImage,
						Image: &catalogd.ImageSource{
							Ref:                   os.Getenv("CATALOG_IMG"),
							InsecureSkipTLSVerify: true,
						},
					},
				},
			}
			t.Logf("When creating an Operator that references a package with a %q bundle type", operator.ObjectMeta.Name)
			require.NoError(t, c.Create(context.Background(), catalog))
			require.NoError(t, c.Create(context.Background(), operator))
			t.Log("It should have a status condition type of Installed with a status of True and a reason of Success")
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				op := &operatorv1alpha1.Operator{}
				assert.NoError(ct, c.Get(context.Background(), client.ObjectKeyFromObject(operator), op))
				cond := meta.FindStatusCondition(op.Status.Conditions, operatorv1alpha1.TypeInstalled)
				if !assert.NotNil(ct, cond) {
					return
				}
				assert.Equal(ct, metav1.ConditionTrue, cond.Status)
				assert.Equal(ct, operatorv1alpha1.ReasonSuccess, cond.Reason)
			}, 2*time.Minute, time.Second)
			require.NoError(t, c.Delete(context.Background(), catalog))
			require.NoError(t, c.Delete(context.Background(), operator))
		})
	}
}
