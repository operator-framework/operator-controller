package action_test

// func Test_ServiceAccountRestConfigMapper(t *testing.T) {
// 	for _, tc := range []struct {
// 		description   string
// 		obj           client.Object
// 		cfg           *rest.Config
// 		expectedError error
// 	}{
// 		{
// 			description:   "return error if object is nil",
// 			cfg:           &rest.Config{},
// 			expectedError: errors.New("object is nil"),
// 		}, {
// 			description:   "return error if cfg is nil",
// 			obj:           &ocv1.ClusterExtension{},
// 			expectedError: errors.New("rest config is nil"),
// 		}, {
// 			description:   "return error if object is not a ClusterExtension",
// 			obj:           &corev1.Secret{},
// 			cfg:           &rest.Config{},
// 			expectedError: errors.New("object is not a ClusterExtension"),
// 		}, {
// 			description: "succeeds if object is not a ClusterExtension",
// 			obj: &ocv1.ClusterExtension{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "my-clusterextension",
// 				},
// 				Spec: ocv1.ClusterExtensionSpec{
// 					ServiceAccount: ocv1.ServiceAccountReference{
// 						Name: "my-service-account",
// 					},
// 					Namespace: "my-namespace",
// 				},
// 			},
// 			cfg: &rest.Config{},
// 		},
// 	} {
// 		t.Run(tc.description, func(t *testing.T) {
// 			tokenGetter := &authentication.TokenGetter{}
// 			saMapper := action.ServiceAccountRestConfigMapper(tokenGetter)
// 			actualCfg, err := saMapper(context.Background(), tc.obj, tc.cfg)
// 			if tc.expectedError != nil {
// 				require.Nil(t, actualCfg)
// 				require.EqualError(t, err, tc.expectedError.Error())
// 			} else {
// 				require.NoError(t, err)
// 				transport, err := rest.TransportFor(actualCfg)
// 				require.NoError(t, err)
// 				require.NotNil(t, transport)
// 				tokenInjectionRoundTripper, ok := transport.(*authentication.TokenInjectingRoundTripper)
// 				require.True(t, ok)
// 				require.Equal(t, tokenGetter, tokenInjectionRoundTripper.TokenGetter)
// 				require.Equal(t, types.NamespacedName{Name: "my-service-account", Namespace: "my-namespace"}, tokenInjectionRoundTripper.Key)
// 			}
// 		})
// 	}
// }
