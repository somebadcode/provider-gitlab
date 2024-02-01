/*
Copyright 2020 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package accesstokens

import (
	"context"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"github.com/xanzy/go-gitlab"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/crossplane-contrib/provider-gitlab/apis/projects/v1alpha1"
	"github.com/crossplane-contrib/provider-gitlab/pkg/clients"
	"github.com/crossplane-contrib/provider-gitlab/pkg/clients/projects"
)

const (
	errNotAccessToken       = "managed resource is not a Gitlab accesstoken custom resource"
	errExternalNameNotInt   = "custom resource external name is not an integer"
	errFailedParseID        = "cannot parse Access Token ID to int"
	errGetFailed            = "cannot get Gitlab accesstoken"
	errCreateFailed         = "cannot create Gitlab accesstoken"
	errDeleteFailed         = "cannot delete Gitlab accesstoken"
	errAccessTokentNotFound = "cannot find Gitlab accesstoken"
	errMissingProjectID     = "missing Spec.ForProvider.ProjectID"
	errRotationFailed       = "access token rotation failed"
)

const (
	// DefaultAccessTokenRotateThreshold is the default period prior to expiration at which a token should be rotated.
	DefaultAccessTokenRotateThreshold = 7 * 24 * time.Hour
)

// SetupAccessToken adds a controller that reconciles ProjectAccessTokens.
func SetupAccessToken(mgr ctrl.Manager, o controller.Options) error {
	name := managed.ControllerName(v1alpha1.AccessTokenKind)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.AccessToken{}).
		Complete(managed.NewReconciler(mgr,
			resource.ManagedKind(v1alpha1.AccessTokenGroupVersionKind),
			managed.WithExternalConnecter(&connector{kube: mgr.GetClient(), newGitlabClientFn: projects.NewAccessTokenClient}),
			managed.WithInitializers(managed.NewDefaultProviderConfig(mgr.GetClient())),
			managed.WithLogger(o.Logger.WithValues("controller", name)),
			managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name)))))
}

type connector struct {
	kube              client.Client
	newGitlabClientFn func(cfg clients.Config) projects.AccessTokenClient
}

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*v1alpha1.AccessToken)
	if !ok {
		return nil, errors.New(errNotAccessToken)
	}
	cfg, err := clients.GetConfig(ctx, c.kube, cr)
	if err != nil {
		return nil, err
	}
	return &external{kube: c.kube, client: c.newGitlabClientFn(*cfg)}, nil
}

type external struct {
	kube   client.Client
	client projects.AccessTokenClient
}

func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1alpha1.AccessToken)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotAccessToken)
	}

	externalName := meta.GetExternalName(cr)
	if externalName == "" {
		return managed.ExternalObservation{}, nil
	}

	accessTokenID, err := strconv.Atoi(externalName)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errFailedParseID)
	}

	if cr.Spec.ForProvider.ProjectID == nil {
		return managed.ExternalObservation{}, errors.New(errMissingProjectID)
	}

	at, res, err := e.client.GetProjectAccessToken(*cr.Spec.ForProvider.ProjectID, accessTokenID, gitlab.WithContext(ctx))
	if err != nil {
		if clients.IsResponseNotFound(res) {
			return managed.ExternalObservation{}, nil
		}
		return managed.ExternalObservation{}, errors.Wrap(err, errAccessTokentNotFound)
	}

	cr.Status.AtProvider.CopyFromToken(at)

	current := cr.Spec.ForProvider.DeepCopy()
	lateInitializeProjectAccessToken(&cr.Spec.ForProvider, at)

	cr.Status.SetConditions(xpv1.Available())

	var threshold = DefaultAccessTokenRotateThreshold
	if current.RotateThreshold != nil {
		threshold = current.RotateThreshold.Abs()
	}

	return managed.ExternalObservation{
		ResourceExists:          true,
		ResourceUpToDate:        !cr.Status.AtProvider.IsRevoked() && !cr.Status.AtProvider.ExpiresWithin(threshold),
		ResourceLateInitialized: !cmp.Equal(current, &cr.Spec.ForProvider),
	}, nil
}

func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1alpha1.AccessToken)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotAccessToken)
	}

	if cr.Spec.ForProvider.ProjectID == nil {
		return managed.ExternalCreation{}, errors.New(errMissingProjectID)
	}

	at, _, err := e.client.CreateProjectAccessToken(
		*cr.Spec.ForProvider.ProjectID,
		projects.GenerateCreateProjectAccessTokenOptions(cr.Name, &cr.Spec.ForProvider),
		gitlab.WithContext(ctx),
	)

	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateFailed)
	}

	cr.Status.AtProvider.CopyFromToken(at)

	meta.SetExternalName(cr, strconv.Itoa(at.ID))

	return managed.ExternalCreation{
		ExternalNameAssigned: true,
		ConnectionDetails: managed.ConnectionDetails{
			"token": []byte(at.Token),
		},
	}, nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1alpha1.AccessToken)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotAccessToken)
	}

	externalName := meta.GetExternalName(cr)
	if externalName == "" {
		return managed.ExternalUpdate{}, errors.New(errNotAccessToken)
	}

	accessTokenID, err := strconv.Atoi(externalName)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errFailedParseID)
	}

	// The next expiration should be as far into the future as the total duration was for previous token.
	// However, the TTL must not be less than twice the rotation threshold.
	// Truncate to 24 hours for consistency, because that's what Gitlab will do anyway.
	ttl := cr.Status.AtProvider.TotalDuration()
	if cr.Spec.ForProvider.RotateThreshold != nil {
		ttl = max(ttl, cr.Spec.ForProvider.RotateThreshold.Duration*time.Duration(2))
	}
	expiresAt := time.Now().Add(ttl).Truncate(24 * time.Hour)

	var at *gitlab.ProjectAccessToken

	at, _, err = e.client.RotateProjectAccessToken(
		*cr.Spec.ForProvider.ProjectID,
		accessTokenID,
		&gitlab.RotateProjectAccessTokenOptions{
			ExpiresAt: gitlab.Ptr(gitlab.ISOTime(expiresAt)),
		},
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errRotationFailed)
	}

	cr.Status.AtProvider.CopyFromToken(at)

	meta.SetExternalName(cr, strconv.Itoa(at.ID))

	return managed.ExternalUpdate{
		ConnectionDetails: managed.ConnectionDetails{
			"token": []byte(at.Token),
		},
	}, nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*v1alpha1.AccessToken)
	if !ok {
		return errors.New(errNotAccessToken)
	}

	accessTokenID, err := strconv.Atoi(meta.GetExternalName(cr))

	if err != nil {
		return errors.New(errExternalNameNotInt)
	}

	if cr.Spec.ForProvider.ProjectID == nil {
		return errors.New(errMissingProjectID)
	}
	_, err = e.client.RevokeProjectAccessToken(
		*cr.Spec.ForProvider.ProjectID,
		accessTokenID,
		gitlab.WithContext(ctx),
	)

	return errors.Wrap(err, errDeleteFailed)
}

// lateInitializeProjectAccessToken fills the empty fields in the access token spec with the
// values seen in gitlab access token.
func lateInitializeProjectAccessToken(in *v1alpha1.AccessTokenParameters, accessToken *gitlab.ProjectAccessToken) { // nolint:gocyclo
	if accessToken == nil {
		return
	}

	if in.AccessLevel == nil {
		in.AccessLevel = (*v1alpha1.AccessLevelValue)(&accessToken.AccessLevel)
	}

	if in.ExpiresAt == nil && accessToken.ExpiresAt != nil {
		in.ExpiresAt = &metav1.Time{Time: time.Time(*accessToken.ExpiresAt)}
	}
}
