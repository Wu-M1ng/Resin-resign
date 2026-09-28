package proxy

import "context"

// InboundPolicy carries endpoint-specific behavior into shared proxy handlers.
type InboundPolicy struct {
	RequireProxyAuthInfo bool
}

// InboundEndpoint identifies the listener that accepted a connection. Scoped
// relay credentials are bound to this endpoint so a Feed token cannot be
// replayed on a different port or on a plaintext listener.
type InboundEndpoint struct {
	ID         string
	Port       int
	TLSEnabled bool
}

type inboundPolicyContextKey struct{}
type inboundEndpointContextKey struct{}

func ContextWithInboundPolicy(ctx context.Context, policy InboundPolicy) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, inboundPolicyContextKey{}, policy)
}

func InboundPolicyFromContext(ctx context.Context) InboundPolicy {
	if ctx == nil {
		return InboundPolicy{}
	}
	policy, _ := ctx.Value(inboundPolicyContextKey{}).(InboundPolicy)
	return policy
}

func ContextWithInboundEndpoint(ctx context.Context, endpoint InboundEndpoint) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, inboundEndpointContextKey{}, endpoint)
}

func InboundEndpointFromContext(ctx context.Context) (InboundEndpoint, bool) {
	if ctx == nil {
		return InboundEndpoint{}, false
	}
	endpoint, ok := ctx.Value(inboundEndpointContextKey{}).(InboundEndpoint)
	if !ok || endpoint.Port < 1 || endpoint.Port > 65535 {
		return InboundEndpoint{}, false
	}
	return endpoint, true
}
