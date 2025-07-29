package oidfed

import (
	"crypto"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/pkg/errors"

	"github.com/go-oidfed/lib/apimodel"
	"github.com/go-oidfed/lib/cache"
	"github.com/go-oidfed/lib/internal"
	"github.com/go-oidfed/lib/jwks"
	"github.com/go-oidfed/lib/unixtime"
)

// FederationEntity is a type for an entity participating in federations.
// It holds all relevant information about the federation entity and can be used to create
// an EntityConfiguration about it
type FederationEntity struct {
	EntityID              string
	Metadata              *Metadata
	AuthorityHints        []string
	ConfigurationLifetime int64
	*EntityStatementSigner
	jwks             jwks.JWKS
	TrustMarks       []*EntityConfigurationTrustMarkConfig
	TrustMarkIssuers AllowedTrustMarkIssuers
	TrustMarkOwners  TrustMarkOwners
	Extra            map[string]any
}

// FederationLeaf is a type for a leaf entity and holds all relevant information about it; it can also be used to
// create an EntityConfiguration about it or to start OIDC flows
type FederationLeaf struct {
	FederationEntity
	TrustAnchors   TrustAnchors
	oidcROProducer *RequestObjectProducer
}

// NewFederationEntity creates a new FederationEntity with the passed properties
func NewFederationEntity(
	entityID string, authorityHints []string, metadata *Metadata,
	signer *EntityStatementSigner, configurationLifetime int64, extra map[string]any,
) (*FederationEntity, error) {
	if configurationLifetime <= 0 {
		configurationLifetime = defaultEntityConfigurationLifetime
	}
	return &FederationEntity{
		EntityID:              entityID,
		Metadata:              metadata,
		AuthorityHints:        authorityHints,
		EntityStatementSigner: signer,
		ConfigurationLifetime: configurationLifetime,
		jwks:                  signer.JWKS(),
		Extra:                 extra,
	}, nil
}

// NewFederationLeaf creates a new FederationLeaf with the passed properties
func NewFederationLeaf(
	entityID string, authorityHints []string, trustAnchors TrustAnchors, metadata *Metadata,
	signer *EntityStatementSigner, configurationLifetime int64,
	oidcSigningKey crypto.Signer, oidcSigningAlg jwa.SignatureAlgorithm, extra map[string]any,
) (*FederationLeaf, error) {
	fed, err := NewFederationEntity(
		entityID, authorityHints, metadata, signer, configurationLifetime, extra,
	)
	if err != nil {
		return nil, err
	}
	return &FederationLeaf{
		FederationEntity: *fed,
		TrustAnchors:     trustAnchors,
		oidcROProducer:   NewRequestObjectProducer(entityID, oidcSigningKey, oidcSigningAlg, 60),
	}, nil
}

// EntityConfigurationPayload returns an EntityStatementPayload for this FederationEntity
func (f FederationEntity) EntityConfigurationPayload() *EntityStatementPayload {
	now := time.Now()
	var tms []TrustMarkInfo
	for _, tmc := range f.TrustMarks {
		tm, err := tmc.TrustMarkJWT()
		if err != nil {
			internal.Log(err.Error())
			continue
		}
		tms = append(
			tms, TrustMarkInfo{
				TrustMarkType: tmc.TrustMarkType,
				TrustMarkJWT:  tm,
			},
		)
	}
	return &EntityStatementPayload{
		Issuer:           f.EntityID,
		Subject:          f.EntityID,
		IssuedAt:         unixtime.Unixtime{Time: now},
		ExpiresAt:        unixtime.Unixtime{Time: now.Add(time.Second * time.Duration(f.ConfigurationLifetime))},
		JWKS:             f.jwks,
		AuthorityHints:   f.AuthorityHints,
		Metadata:         f.Metadata,
		TrustMarks:       tms,
		TrustMarkIssuers: f.TrustMarkIssuers,
		TrustMarkOwners:  f.TrustMarkOwners,
		Extra:            f.Extra,
	}
}

// EntityConfigurationJWT creates and returns the signed jwt as a []byte for
// the entity's entity configuration
func (f FederationEntity) EntityConfigurationJWT() ([]byte, error) {
	return f.EntityStatementSigner.JWT(f.EntityConfigurationPayload())
}

// SignEntityStatement creates a signed JWT for the given EntityStatementPayload; this function is intended to be
// used on TA/IA
func (f FederationEntity) SignEntityStatement(payload EntityStatementPayload) ([]byte, error) {
	return f.EntityStatementSigner.JWT(payload)
}

// RequestObjectProducer returns the entity's RequestObjectProducer
func (f FederationLeaf) RequestObjectProducer() *RequestObjectProducer {
	return f.oidcROProducer
}

// ResolveOPMetadata resolves and returns OpenIDProviderMetadata for the
// passed issuer url
func (f FederationLeaf) ResolveOPMetadata(issuer string) (*OpenIDProviderMetadata, error) {
	var opm OpenIDProviderMetadata
	set, err := cache.Get(cache.Key(cache.KeyOPMetadata, issuer), &opm)
	if err != nil {
		log.Errorf("Failed to get cached OP metadata for issuer %s: %v", issuer, err)
	} else if set {
		// Validate cached metadata
		if opm.TokenEndpoint == "" {
			log.Warnf("Cached OP metadata for issuer %s is missing TokenEndpoint, fetching fresh metadata", issuer)
		} else {
			log.Infof("Using cached OP metadata for issuer %s", issuer)
			return &opm, nil
		}
	} else {
		log.Infof("No cached OP metadata found for issuer %s, fetching fresh metadata", issuer)
	}

	metadata, err := DefaultMetadataResolver.Resolve(
		apimodel.ResolveRequest{
			Subject:     issuer,
			TrustAnchor: f.TrustAnchors.EntityIDs(),
			EntityTypes: []string{"openid_provider"},
		},
	)
	if err != nil {
		log.Errorf("Failed to resolve OP metadata for issuer %s: %v", issuer, err)
		return nil, errors.Wrap(err, "no trust chain with valid metadata found")
	}

	log.Infof("Successfully resolved fresh OP metadata for issuer %s", issuer)
	return metadata.OpenIDProvider, nil
}

