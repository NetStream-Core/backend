package api

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
)

const identityContextKey = "identity"

// Identity is the authenticated caller of an mTLS-protected request,
// derived from its client certificate: CommonName names the caller (a
// sensor's host_id, or an operator's own name), and IsOperator comes from
// whether the certificate's OrganizationalUnit contains "operator" — any
// other (or absent) OU is treated as a sensor, scoped to its own host_id.
type Identity struct {
	CommonName string
	IsOperator bool
}

func clientIdentity(ctx *gin.Context) (Identity, bool) {
	if ctx.Request.TLS == nil || len(ctx.Request.TLS.PeerCertificates) == 0 {
		return Identity{}, false
	}
	cert := ctx.Request.TLS.PeerCertificates[0]
	return Identity{
		CommonName: cert.Subject.CommonName,
		IsOperator: slices.Contains(cert.Subject.OrganizationalUnit, "operator"),
	}, true
}

// requireClientCert rejects any request with no verified client
// certificate and stores the resulting Identity for later middleware and
// handlers. The TLS handshake itself already verified the certificate
// against the configured CA before the request reaches here — this only
// checks that a certificate was actually presented.
func requireClientCert() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		identity, ok := clientIdentity(ctx)
		if !ok {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "client certificate required"})
			return
		}
		ctx.Set(identityContextKey, identity)
		ctx.Next()
	}
}

// requireOperator additionally rejects a verified sensor identity, for
// routes a sensor should never reach even for its own host_id (model
// publication, the sensor registry as a whole).
func requireOperator() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		identity := ctx.MustGet(identityContextKey).(Identity) //nolint:forcetypeassert // set by requireClientCert earlier in the chain
		if !identity.IsOperator {
			ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "operator role required"})
			return
		}
		ctx.Next()
	}
}

// requireSelfOrOperator lets an operator act on any sensor, but restricts
// a sensor identity to the `:host_id` path parameter matching its own
// CommonName — one sensor's certificate can't read or ack another
// sensor's configuration.
func requireSelfOrOperator() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		identity := ctx.MustGet(identityContextKey).(Identity) //nolint:forcetypeassert // set by requireClientCert earlier in the chain
		if !identity.IsOperator && identity.CommonName != ctx.Param("host_id") {
			ctx.AbortWithStatusJSON(
				http.StatusForbidden,
				gin.H{"error": "sensors may only access their own resources"},
			)
			return
		}
		ctx.Next()
	}
}
