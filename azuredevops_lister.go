package githosts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/go-retryablehttp"
	"gitlab.com/tozd/go/errors"
)

var _ Lister = (*AzureDevOpsHost)(nil)

const (
	// azureDevOpsAPIVersion is the REST API version the lister requests.
	azureDevOpsAPIVersion = "7.1"
	// azureDevOpsEntitlementsDomain serves the Member Entitlement Management
	// API, which lives on a different host from the core and Git APIs.
	azureDevOpsEntitlementsDomain = "vsaex.dev.azure.com"
	// azureDevOpsContinuationHeader carries the Projects - List continuation
	// token.
	azureDevOpsContinuationHeader = "X-Ms-Continuationtoken"
	azureDevOpsVisibilityPublic   = "public"
)

// azureDevOpsListedRepo is a GitRepository from Repositories - List, with the
// fields Backup does not use.
// https://learn.microsoft.com/en-us/rest/api/azure/devops/git/repositories/list?view=azure-devops-rest-7.1
type azureDevOpsListedRepo struct {
	AzureDevOpsRepo
	IsFork     bool `json:"isFork"`
	IsDisabled bool `json:"isDisabled"`
}

type azureDevOpsListedRepos struct {
	Value []azureDevOpsListedRepo `json:"value"`
}

type azureDevOpsProjects struct {
	Value []Project `json:"value"`
}

// azureDevOpsUserEntitlements is a page of User Entitlements - Search User
// Entitlements.
// https://learn.microsoft.com/en-us/rest/api/azure/devops/memberentitlementmanagement/user-entitlements/search-user-entitlements?view=azure-devops-rest-7.1
type azureDevOpsUserEntitlements struct {
	ContinuationToken string `json:"continuationToken"`
	Items             []struct {
		User struct {
			PrincipalName string `json:"principalName"`
			MailAddress   string `json:"mailAddress"`
		} `json:"user"`
	} `json:"items"`
}

// cloneAuth returns the credentials Backup clones with: the configured
// username and the personal access token.
func (ad *AzureDevOpsHost) cloneAuth() BasicAuth {
	return BasicAuth{User: ad.UserName, Password: ad.PAT}
}

func (ad *AzureDevOpsHost) listingClient() *retryablehttp.Client {
	if ad.HttpClient != nil {
		return ad.HttpClient
	}

	return getHTTPClient()
}

// azureDevOpsGet requests reqURL with the PAT and decodes a 200 response into
// out, returning the response headers. Azure DevOps answers an invalid PAT
// with 203 and a sign-in page, so anything but 200 is an error.
func (ad *AzureDevOpsHost) azureDevOpsGet(ctx context.Context, reqURL, what string, out any) (http.Header, errors.E) {
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}

	req.Header.Set(HeaderAccept, ContentTypeJSON)
	req.Header.Set(HeaderAuthorization, AuthPrefixBasic+generateBasicAuth(ad.UserName, ad.PAT))

	resp, err := ad.listingClient().Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get %s", what)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read %s response", what)
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNonAuthoritativeInfo, http.StatusUnauthorized:
		return nil, errors.Errorf("Azure DevOps authentication failed getting %s (HTTP %d)", what, resp.StatusCode)
	case http.StatusNotFound:
		return nil, errors.Errorf("Azure DevOps %s not found (HTTP 404)", what)
	default:
		return nil, errors.Errorf("failed to get Azure DevOps %s: %d (%s)", what, resp.StatusCode, resp.Status)
	}

	if uErr := json.Unmarshal(body, out); uErr != nil {
		return nil, errors.Wrapf(uErr, "failed to unmarshal Azure DevOps %s response", what)
	}

	return resp.Header, nil
}

// listOrgProjects lists the organisation's well-formed projects, the default
// state filter of Projects - List and so the same set Backup walks.
// https://learn.microsoft.com/en-us/rest/api/azure/devops/core/projects/list?view=azure-devops-rest-7.1
func (ad *AzureDevOpsHost) listOrgProjects(ctx context.Context, org string) ([]Project, errors.E) {
	var projects []Project

	continuation := ""

	for {
		params := url.Values{"api-version": {azureDevOpsAPIVersion}}
		if continuation != "" {
			params.Set("continuationToken", continuation)
		}

		reqURL := "https://" + azureDevOpsDomain + "/" + url.PathEscape(org) + "/_apis/projects?" + params.Encode()

		var page azureDevOpsProjects

		header, err := ad.azureDevOpsGet(ctx, reqURL, "organization "+org+" projects", &page)
		if err != nil {
			return nil, err
		}

		projects = append(projects, page.Value...)

		continuation = header.Get(azureDevOpsContinuationHeader)
		if continuation == "" {
			return projects, nil
		}
	}
}

// ListOwnRepos lists the repositories Backup processes: those of the first
// configured organisation, as Backup does not support more than one.
func (ad *AzureDevOpsHost) ListOwnRepos(ctx context.Context) ([]Repository, error) {
	if len(ad.Orgs) == 0 {
		return nil, errors.New("no organizations specified")
	}

	return ad.ListOrgRepos(ctx, ad.Orgs[0])
}

// ListUserRepos is not supported: Azure DevOps repositories belong to
// projects within an organisation, never to a user.
func (ad *AzureDevOpsHost) ListUserRepos(context.Context, string) ([]Repository, error) {
	return nil, errors.WithMessage(ErrNotSupported, "Azure DevOps repositories belong to projects, not users")
}

// ListOrgRepos lists the Git repositories of every project in the
// organisation, as Backup does. Hidden repositories are excluded, as they are
// by default from the API and from Backup.
//
// Fields are filled from Repositories - List and the project's entry in
// Projects - List:
//   - Owner is the organisation and PathWithNamespace is
//     "organisation/project/repository", matching Backup.
//   - CloneURL is webUrl, which Backup clones. remoteUrl is not used as it
//     carries the organisation as a URL username.
//   - IsArchived is always false. Azure DevOps has no archived state; the
//     nearest, isDisabled, blocks all access to the repository rather than
//     making it read-only, so it is not reported as archived.
//   - IsEmpty reports that there is no defaultBranch, which Azure DevOps only
//     sets once the first branch is pushed.
//   - IsPrivate reports that the project's visibility is not public.
//   - SizeKB is the compressed size in bytes rounded up to whole kilobytes.
func (ad *AzureDevOpsHost) ListOrgRepos(ctx context.Context, org string) ([]Repository, error) {
	if org == "" {
		return nil, errors.New("organization not specified")
	}

	projects, err := ad.listOrgProjects(ctx, org)
	if err != nil {
		return nil, err
	}

	auth := ad.cloneAuth()

	var out []Repository

	for _, project := range projects {
		ref := project.Id
		if ref == "" {
			ref = project.Name
		}

		reqURL := "https://" + azureDevOpsDomain + "/" + url.PathEscape(org) + "/" + url.PathEscape(ref) +
			"/_apis/git/repositories?api-version=" + azureDevOpsAPIVersion

		var page azureDevOpsListedRepos

		if _, err = ad.azureDevOpsGet(ctx, reqURL, "organization "+org+" project "+project.Name+" repositories", &page); err != nil {
			return nil, err
		}

		for _, r := range page.Value {
			out = append(out, azureDevOpsRepository(org, project, r, auth))
		}
	}

	return out, nil
}

func azureDevOpsRepository(org string, project Project, r azureDevOpsListedRepo, auth BasicAuth) Repository {
	projectName := r.Project.Name
	if projectName == "" {
		projectName = project.Name
	}

	visibility := project.Visibility
	if visibility == "" {
		visibility = r.Project.Visibility
	}

	return Repository{
		Owner:             org,
		Name:              r.Name,
		PathWithNamespace: org + "/" + projectName + "/" + r.Name,
		Domain:            azureDevOpsDomain,
		CloneURL:          withoutUserInfo(r.WebUrl),
		SSHURL:            r.SshUrl,
		Auth:              auth,
		IsFork:            r.IsFork,
		IsEmpty:           r.DefaultBranch == "",
		IsPrivate:         !strings.EqualFold(visibility, azureDevOpsVisibilityPublic),
		SizeKB:            (r.Size + 1023) / 1024, //nolint:mnd // bytes to kilobytes, rounding up
	}
}

// withoutUserInfo strips any username or password from rawURL, so that a
// clone URL can never carry credentials.
func withoutUserInfo(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return rawURL
	}

	u.User = nil

	return u.String()
}

// ListOrgMembers returns the principal names of the organisation's users,
// falling back to the mail address for a user without one. Azure DevOps
// identifies users by principal name: the sign-in address of a Microsoft
// Entra ID or Microsoft account.
//
// It uses User Entitlements - Search User Entitlements, so the PAT needs the
// Member Entitlement Management (Read) scope, vso.memberentitlementmanagement,
// which Backup does not.
func (ad *AzureDevOpsHost) ListOrgMembers(ctx context.Context, org string) ([]string, error) {
	if org == "" {
		return nil, errors.New("organization not specified")
	}

	var members []string

	continuation := ""

	for {
		params := url.Values{"api-version": {azureDevOpsAPIVersion}}
		if continuation != "" {
			params.Set("continuationToken", continuation)
		}

		reqURL := "https://" + azureDevOpsEntitlementsDomain + "/" + url.PathEscape(org) + "/_apis/userentitlements?" + params.Encode()

		var page azureDevOpsUserEntitlements

		if _, err := ad.azureDevOpsGet(ctx, reqURL, "organization "+org+" user entitlements", &page); err != nil {
			return nil, err
		}

		for _, item := range page.Items {
			name := item.User.PrincipalName
			if name == "" {
				name = item.User.MailAddress
			}

			if name != "" {
				members = append(members, name)
			}
		}

		// guard against a server repeating its token, which would never end
		if page.ContinuationToken == "" || page.ContinuationToken == continuation || len(page.Items) == 0 {
			return members, nil
		}

		continuation = page.ContinuationToken
	}
}
