package api

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
)

const identityContextKey = "identity"

// Identity is the authenticated caller of an mTLS-protected request,
// derived from its client certificate: CommonName names the caller (a
// sensor's host_id, or an operator's own name). OrganizationalUnit maps
// to a role: "operator" is a full operator (read and write), "operator-
// readonly" is an operator for read access only (IsOperator true, but
// CanWrite false) — for an auditor or a dashboard that should see
// sensors/models/incidents without being able to change configuration,
// publish models, or close incidents. Any other (or absent) OU is a
// sensor, scoped to its own host_id.
type Identity struct {
	CommonName string
	IsOperator bool
	CanWrite   bool
}

func clientIdentity(ctx *gin.Context) (Identity, bool) {
	if ctx.Request.TLS == nil || len(ctx.Request.TLS.PeerCertificates) == 0 {
		return Identity{}, false
	}
	cert := ctx.Request.TLS.PeerCertificates[0]
	ou := cert.Subject.OrganizationalUnit
	fullOperator := slices.Contains(ou, "operator")
	readOnlyOperator := slices.Contains(ou, "operator-readonly")
	return Identity{
		CommonName: cert.Subject.CommonName,
		IsOperator: fullOperator || readOnlyOperator,
		CanWrite:   fullOperator,
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
// publication, the sensor registry as a whole). A read-only operator
// passes this check too — it only screens out sensors, not write access;
// routes that mutate state also need requireWriteOperator.
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

// requireWriteOperator rejects a read-only operator (OrganizationalUnit
// "operator-readonly") the same way requireOperator rejects a sensor —
// for routes that change state (publish a model, set a sensor's desired
// config, close an incident) rather than just read it.
func requireWriteOperator() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		identity := ctx.MustGet(identityContextKey).(Identity) //nolint:forcetypeassert // set by requireClientCert earlier in the chain
		if !identity.CanWrite {
			ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "write access required"})
			return
		}
		ctx.Next()
	}
}

// requireSelfOrOperator lets any operator (read-only included) act on any
// sensor for a read, but restricts a sensor identity to the `:host_id`
// path parameter matching its own CommonName — one sensor's certificate
// can't read another sensor's configuration.
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

// requireSelfOrWriteOperator is requireSelfOrOperator's write-side
// counterpart, for a sensor acknowledging its own config: a sensor acting
// on itself is always allowed (that's the whole point of self), but
// acting on another sensor's behalf needs a full operator, not a
// read-only one.
func requireSelfOrWriteOperator() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		identity := ctx.MustGet(identityContextKey).(Identity) //nolint:forcetypeassert // set by requireClientCert earlier in the chain
		if identity.CommonName != ctx.Param("host_id") && !identity.CanWrite {
			ctx.AbortWithStatusJSON(
				http.StatusForbidden,
				gin.H{"error": "sensors may only act on their own resources"},
			)
			return
		}
		ctx.Next()
	}
}
