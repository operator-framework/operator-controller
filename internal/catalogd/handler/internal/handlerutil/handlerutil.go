package handlerutil

import (
	"net/http"

	"k8s.io/apimachinery/pkg/util/sets"
)

func AllowedMethodsHandler(next http.Handler, allowedMethods ...string) http.Handler {
	allowedMethodSet := sets.New[string](allowedMethods...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedMethodSet.Has(r.Method) {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}
