package object

import (
	"fmt"
	"net/url"

	apigatewayv1beta1 "github.com/kyma-project/api-gateway/apis/gateway/v1beta1"
	kmetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	eventingv1alpha2 "github.com/kyma-project/eventing-manager/api/eventing/v1alpha2"
	"github.com/kyma-project/eventing-manager/pkg/featureflags"
)

const (
	// OAuthHandlerNameOAuth2Introspection OAuth handler name supported in Kyma for oauth2_introspection.
	OAuthHandlerNameOAuth2Introspection = "oauth2_introspection"

	// OAuthHandlerNameJWT OAuth handler name supported in Kyma for jwt.
	OAuthHandlerNameJWT = "jwt"

	// JWKSURLFormat the format of the jwks URL.
	JWKSURLFormat = `{"jwks_urls":["%s"]}`
)

// NewAPIRule creates a APIRule object.
func NewAPIRule(ns, namePrefix string, opts ...Option) *apigatewayv1beta1.APIRule {
	s := &apigatewayv1beta1.APIRule{
		ObjectMeta: kmetav1.ObjectMeta{
			Namespace:    ns,
			GenerateName: namePrefix,
		},
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// ApplyExistingAPIRuleAttributes copies some important attributes from a given
// source APIRule to a destination APIRule.
func ApplyExistingAPIRuleAttributes(src, dst *apigatewayv1beta1.APIRule) {
	// resourceVersion must be returned to the API server
	// unmodified for optimistic concurrency, as per Kubernetes API
	// conventions
	dst.Name = src.Name
	dst.GenerateName = ""
	dst.ResourceVersion = src.ResourceVersion
	dst.Spec.Host = src.Spec.Host
	// preserve status to avoid resetting conditions
	dst.Status = src.Status
}

func GetService(svcName string, port uint32) apigatewayv1beta1.Service {
	isExternal := true
	return apigatewayv1beta1.Service{
		Name:       &svcName,
		Port:       &port,
		IsExternal: &isExternal,
	}
}

// WithService sets the Service of an APIRule.
func WithService(host, svcName string, port uint32) Option {
	return func(r *apigatewayv1beta1.APIRule) {
		apiService := GetService(svcName, port)
		r.Spec.Service = &apiService
		r.Spec.Host = &host
	}
}

// WithGateway sets the gateway of an APIRule.
func WithGateway(gw string) Option {
	return func(r *apigatewayv1beta1.APIRule) {
		r.Spec.Gateway = &gw
	}
}

// RemoveDuplicateValues appends the values if the key (values of the slice) is not equal
// to the already present value in new slice (list).
func RemoveDuplicateValues(values []string) []string {
	keys := make(map[string]bool)
	list := make([]string, 0)

	for _, entry := range values {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

// WithLabels sets the labels for an APIRule.
func WithLabels(labels map[string]string) Option {
	return func(r *apigatewayv1beta1.APIRule) {
		r.SetLabels(labels)
	}
}

// WithOwnerReference sets the OwnerReferences of an APIRule.
func WithOwnerReference(subs []eventingv1alpha2.Subscription) Option {
	return func(r *apigatewayv1beta1.APIRule) {
		ownerRefs := make([]kmetav1.OwnerReference, 0)
		for _, sub := range subs {
			blockOwnerDeletion := true
			ownerRef := kmetav1.OwnerReference{
				APIVersion:         sub.APIVersion,
				Kind:               sub.Kind,
				Name:               sub.Name,
				UID:                sub.UID,
				BlockOwnerDeletion: &blockOwnerDeletion,
			}
			ownerRefs = append(ownerRefs, ownerRef)
		}
		r.SetOwnerReferences(ownerRefs)
	}
}

// WithRules sets the rules of an APIRule for all Subscriptions for a subscriber.
func WithRules(certsURL string, subs []eventingv1alpha2.Subscription, svc apigatewayv1beta1.Service,
	methods ...string,
) Option {
	return func(r *apigatewayv1beta1.APIRule) {
		var handler apigatewayv1beta1.Handler
		if featureflags.IsEventingWebhookAuthEnabled() {
			handler.Name = OAuthHandlerNameJWT
			handler.Config = &runtime.RawExtension{
				Raw: []byte(fmt.Sprintf(JWKSURLFormat, certsURL)),
			}
		} else {
			handler.Name = OAuthHandlerNameOAuth2Introspection
		}
		authenticator := &apigatewayv1beta1.Authenticator{
			Handler: &handler,
		}
		accessStrategies := []*apigatewayv1beta1.Authenticator{
			authenticator,
		}
		rules := make([]apigatewayv1beta1.Rule, 0)
		paths := make([]string, 0)
		for _, sub := range subs {
			hostURL, err := url.ParseRequestURI(sub.Spec.Sink)
			if err != nil {
				// It's ok as the relevant subscription will have a valid cluster local URL in the same namespace.
				continue
			}
			if hostURL.Path == "" {
				paths = append(paths, "/")
			} else {
				paths = append(paths, hostURL.Path)
			}
		}
		uniquePaths := RemoveDuplicateValues(paths)
		for _, path := range uniquePaths {
			rule := apigatewayv1beta1.Rule{
				Path:             path,
				Methods:          StringsToMethods(methods),
				AccessStrategies: accessStrategies,
				Service:          &svc,
			}
			rules = append(rules, rule)
		}
		r.Spec.Rules = rules
	}
}

// StringsToMethods converts a slice of strings into a slice of HttpMethod as defined by api-gateway.
func StringsToMethods(methods []string) []apigatewayv1beta1.HttpMethod {
	httpMethodes := []apigatewayv1beta1.HttpMethod{}
	for _, m := range methods {
		httpMethodes = append(httpMethodes, apigatewayv1beta1.HttpMethod(m))
	}
	return httpMethodes
}
