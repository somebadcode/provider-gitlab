package v1alpha1_test

import (
	"testing"
	"time"

	"github.com/xanzy/go-gitlab"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/crossplane-contrib/provider-gitlab/apis/projects/v1alpha1"
)

func TestAccessTokenObservation_IsRevoked(t *testing.T) {
	type fields struct {
		TokenID   *int
		ExpiresAt *metav1.Time
		CreatedAt *metav1.Time
		Name      *string
		Revoked   *bool
		Active    *bool
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "nil",
			want: false,
		},
		{
			name: "false",
			fields: fields{
				Revoked: gitlab.Ptr(false),
			},
			want: false,
		},
		{
			name: "true",
			fields: fields{
				Revoked: gitlab.Ptr(true),
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at := &v1alpha1.AccessTokenObservation{
				TokenID:   tt.fields.TokenID,
				ExpiresAt: tt.fields.ExpiresAt,
				CreatedAt: tt.fields.CreatedAt,
				Name:      tt.fields.Name,
				Revoked:   tt.fields.Revoked,
				Active:    tt.fields.Active,
			}
			if got := at.IsRevoked(); got != tt.want {
				t.Errorf("IsRevoked() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAccessTokenObservation_ExpiresWithin(t *testing.T) {
	type fields struct {
		TokenID   *int
		ExpiresAt *metav1.Time
		CreatedAt *metav1.Time
		Name      *string
		Revoked   *bool
		Active    *bool
	}
	type args struct {
		d time.Duration
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "nil",
			args: args{
				d: 48 * time.Hour,
			},
		},
		{
			name: "7d_48h_threshold",
			fields: fields{
				ExpiresAt: gitlab.Ptr(metav1.NewTime(time.Now().Add(7 * 24 * time.Hour).Truncate(24 * time.Hour))),
			},
			args: args{
				d: 48 * time.Hour,
			},
		},
		{
			name: "7d_8d_threshold",
			fields: fields{
				ExpiresAt: gitlab.Ptr(metav1.NewTime(time.Now().Add(7 * 24 * time.Hour).Truncate(24 * time.Hour))),
			},
			args: args{
				d: 8 * 24 * time.Hour,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at := &v1alpha1.AccessTokenObservation{
				TokenID:   tt.fields.TokenID,
				ExpiresAt: tt.fields.ExpiresAt,
				CreatedAt: tt.fields.CreatedAt,
				Name:      tt.fields.Name,
				Revoked:   tt.fields.Revoked,
				Active:    tt.fields.Active,
			}
			if got := at.ExpiresWithin(tt.args.d); got != tt.want {
				t.Errorf("ExpiresWithin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAccessTokenObservation_TTL(t *testing.T) {
	type fields struct {
		TokenID   *int
		ExpiresAt *metav1.Time
		CreatedAt *metav1.Time
		Name      *string
		Revoked   *bool
		Active    *bool
	}
	tests := []struct {
		name   string
		fields fields
		want   time.Duration
	}{
		{
			name: "nil",
			want: v1alpha1.DefaultAccessTokenMaxDuration,
		},
		{
			name: "created_nil_expires_set",
			fields: fields{
				ExpiresAt: gitlab.Ptr(metav1.NewTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))),
			},
			want: v1alpha1.DefaultAccessTokenMaxDuration,
		},
		{
			name: "created_set_expires_nil",
			fields: fields{
				CreatedAt: gitlab.Ptr(metav1.NewTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))),
			},
			want: v1alpha1.DefaultAccessTokenMaxDuration,
		},
		{
			name: "48h",
			fields: fields{
				ExpiresAt: gitlab.Ptr(metav1.NewTime(time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC))),
				CreatedAt: gitlab.Ptr(metav1.NewTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))),
			},
			want: 48 * time.Hour,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at := &v1alpha1.AccessTokenObservation{
				TokenID:   tt.fields.TokenID,
				ExpiresAt: tt.fields.ExpiresAt,
				CreatedAt: tt.fields.CreatedAt,
				Name:      tt.fields.Name,
				Revoked:   tt.fields.Revoked,
				Active:    tt.fields.Active,
			}
			if got := at.TotalDuration(); got != tt.want {
				t.Errorf("TotalDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}
