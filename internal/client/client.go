// Package client implements the BeadHub HTTP client.
//
// The client handles all communication with the BeadHub server:
// - POST /v1/bdh/command - Pre-flight approval for bd commands
// - POST /v1/bdh/sync - Sync issues.jsonl after mutations
// - POST /v1/projects/ensure - Get or create project by slug
// - Messaging endpoints (:mail --inbox, :mail --send)
// - Escalation endpoints (:escalate)
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"time"
)

// DefaultTimeout is the default HTTP request timeout.
const DefaultTimeout = 10 * time.Second

// maxResponseSize limits response body reads to prevent memory exhaustion.
const maxResponseSize = 10 * 1024 * 1024 // 10MB

// Client is the BeadHub HTTP client.
type Client struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string // API key for Bearer auth
}

// New creates a new BeadHub client.
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// NewWithAPIKey creates a new BeadHub client with API key authentication.
// When an API key is set, all requests include an Authorization: Bearer header.
func NewWithAPIKey(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		apiKey: apiKey,
	}
}

// CommandRequest is the request body for /v1/bdh/command.
type CommandRequest struct {
	WorkspaceID string `json:"workspace_id"`
	RepoID      string `json:"repo_id,omitempty"`
	Alias       string `json:"alias"`
	HumanName   string `json:"human_name"`
	RepoOrigin  string `json:"repo_origin"`
	Role        string `json:"role,omitempty"`
	CommandLine string `json:"command_line"`
}

// CommandResponse is the response from /v1/bdh/command.
type CommandResponse struct {
	Approved bool            `json:"approved"`
	Reason   string          `json:"reason,omitempty"`
	Context  *CommandContext `json:"context,omitempty"`
}

// CommandContext contains coordination context returned by the server.
type CommandContext struct {
	MessagesWaiting int              `json:"messages_waiting"`
	BeadsInProgress []BeadInProgress `json:"beads_in_progress"`
}

// BeadInProgress represents a bead being worked on by another workspace.
type BeadInProgress struct {
	BeadID      string `json:"bead_id"`
	WorkspaceID string `json:"workspace_id"`
	Alias       string `json:"alias"`
	HumanName   string `json:"human_name"`
	StartedAt   string `json:"started_at"`
	Title       string `json:"title,omitempty"`
	Role        string `json:"role,omitempty"`
}

// Command sends a command request to the BeadHub server for approval.
func (c *Client) Command(ctx context.Context, req *CommandRequest) (*CommandResponse, error) {
	var resp CommandResponse
	if err := c.post(ctx, "/v1/bdh/command", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SyncRequest is the request body for /v1/bdh/sync.
// Supports two modes:
//   - Full sync: IssuesJSONL contains all issues (SyncMode empty or "full")
//   - Incremental: SyncMode="incremental", ChangedIssues contains modified issues,
//     DeletedIDs contains IDs to remove
type SyncRequest struct {
	WorkspaceID string `json:"workspace_id"`
	RepoID      string `json:"repo_id,omitempty"`
	Alias       string `json:"alias"`
	HumanName   string `json:"human_name"`
	RepoOrigin  string `json:"repo_origin"`
	Role        string `json:"role,omitempty"`
	CommandLine string `json:"command_line,omitempty"`

	// Full sync mode (send everything)
	IssuesJSONL string `json:"issues_jsonl,omitempty"`

	// Incremental sync mode
	SyncMode      string   `json:"sync_mode,omitempty"`      // "full" or "incremental"
	ChangedIssues string   `json:"changed_issues,omitempty"` // JSONL of changed/new issues
	DeletedIDs    []string `json:"deleted_ids,omitempty"`    // IDs of deleted issues

	// Sync protocol negotiation (optional; enables safe schema evolution/backfills)
	SyncProtocolVersion *int `json:"sync_protocol_version,omitempty"`
}

// SyncStats contains detailed statistics from a sync operation.
type SyncStats struct {
	Received int `json:"received"` // Issues received from client
	Inserted int `json:"inserted"` // New issues added
	Updated  int `json:"updated"`  // Existing issues updated
	Deleted  int `json:"deleted"`  // Issues deleted (incremental only)
}

// SyncResponse is the response from /v1/bdh/sync.
type SyncResponse struct {
	Synced      bool            `json:"synced"`
	IssuesCount int             `json:"issues_count"`
	Context     *CommandContext `json:"context,omitempty"`

	// Detailed sync statistics
	Stats *SyncStats `json:"stats,omitempty"`

	SyncProtocolVersion int `json:"sync_protocol_version,omitempty"`
}

// Sync uploads the issues.jsonl to the BeadHub server.
func (c *Client) Sync(ctx context.Context, req *SyncRequest) (*SyncResponse, error) {
	var resp SyncResponse
	if err := c.post(ctx, "/v1/bdh/sync", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// EnsureProjectRequest is the request body for /v1/projects/ensure.
type EnsureProjectRequest struct {
	Slug string `json:"slug"`
}

// EnsureProjectResponse is the response from /v1/projects/ensure.
type EnsureProjectResponse struct {
	ProjectID string `json:"project_id"`
	Slug      string `json:"slug"`
	Created   bool   `json:"created"`
}

// EnsureProject gets or creates a project by slug.
func (c *Client) EnsureProject(ctx context.Context, req *EnsureProjectRequest) (*EnsureProjectResponse, error) {
	var resp EnsureProjectResponse
	if err := c.post(ctx, "/v1/projects/ensure", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// InitRequest is the request body for POST /v1/init.
type InitRequest struct {
	RepoOrigin    string  `json:"repo_origin"`
	Alias         *string `json:"alias,omitempty"`
	HumanName     string  `json:"human_name,omitempty"`
	Role          string  `json:"role,omitempty"`
	ProjectSlug   string  `json:"project_slug,omitempty"`
	Email         string  `json:"email,omitempty"` // For Cloud (ignored by OSS)
	Hostname      string  `json:"hostname,omitempty"`
	WorkspacePath string  `json:"workspace_path,omitempty"`
}

// InitResponse is the response from POST /v1/init.
type InitResponse struct {
	Status           string `json:"status"` // "ok" or "pending_validation"
	CreatedAt        string `json:"created_at"`
	APIKey           string `json:"api_key"`
	ProjectID        string `json:"project_id"`
	ProjectSlug      string `json:"project_slug"`
	AgentID          string `json:"agent_id"`
	RepoID           string `json:"repo_id,omitempty"`
	CanonicalOrigin  string `json:"canonical_origin,omitempty"`
	WorkspaceID      string `json:"workspace_id,omitempty"`
	Alias            string `json:"alias"`
	Created          bool   `json:"created"`           // True if aweb agent was newly created
	WorkspaceCreated bool   `json:"workspace_created"` // True if BeadHub workspace was newly created
}

// Init atomically creates project/repo/workspace/api_key via POST /v1/init.
// Returns nil response for 422 project_not_found (caller should prompt for project_slug).
func (c *Client) Init(ctx context.Context, req *InitRequest) (*InitResponse, error) {
	var resp InitResponse
	if err := c.post(ctx, "/v1/init", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ProjectSummary represents a project in the list response.
type ProjectSummary struct {
	ID             string `json:"id"`
	Slug           string `json:"slug"`
	Name           string `json:"name,omitempty"`
	CreatedAt      string `json:"created_at"`
	RepoCount      int    `json:"repo_count"`
	WorkspaceCount int    `json:"workspace_count"`
}

// ListProjectsResponse is the response from GET /v1/projects.
type ListProjectsResponse struct {
	Projects []ProjectSummary `json:"projects"`
}

// ListProjects lists all projects from the BeadHub server.
func (c *Client) ListProjects(ctx context.Context) (*ListProjectsResponse, error) {
	var resp ListProjectsResponse
	if err := c.get(ctx, "/v1/projects", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteProjectResponse is the response from DELETE /v1/projects/{id}.
type DeleteProjectResponse struct {
	ID                string `json:"id"`
	ReposDeleted      int    `json:"repos_deleted"`
	WorkspacesDeleted int    `json:"workspaces_deleted"`
	ClaimsDeleted     int    `json:"claims_deleted"`
	PresenceCleared   int    `json:"presence_cleared"`
}

// DeleteProject deletes a project by its ID. This cascades to repos and workspaces.
func (c *Client) DeleteProject(ctx context.Context, projectID string) (*DeleteProjectResponse, error) {
	var resp DeleteProjectResponse
	if err := c.delete(ctx, "/v1/projects/"+url.PathEscape(projectID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// EnsureRepoRequest is the request body for /v1/repos/ensure.
type EnsureRepoRequest struct {
	ProjectID string `json:"project_id"`
	OriginURL string `json:"origin_url"`
}

// EnsureRepoResponse is the response from /v1/repos/ensure.
type EnsureRepoResponse struct {
	RepoID          string `json:"repo_id"`
	CanonicalOrigin string `json:"canonical_origin"`
	Name            string `json:"name"`
	Created         bool   `json:"created"`
}

// EnsureRepo gets or creates a repo by origin URL.
func (c *Client) EnsureRepo(ctx context.Context, req *EnsureRepoRequest) (*EnsureRepoResponse, error) {
	var resp EnsureRepoResponse
	if err := c.post(ctx, "/v1/repos/ensure", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// LookupRepoRequest is the request body for /v1/repos/lookup.
type LookupRepoRequest struct {
	OriginURL string `json:"origin_url"`
}

// LookupRepoResponse is the response from /v1/repos/lookup.
type LookupRepoResponse struct {
	RepoID          string `json:"repo_id"`
	ProjectID       string `json:"project_id"`
	ProjectSlug     string `json:"project_slug"`
	CanonicalOrigin string `json:"canonical_origin"`
	Name            string `json:"name"`
}

// LookupRepo looks up a repo by origin URL.
// Returns the repo and its project if found, nil if not found (404).
func (c *Client) LookupRepo(ctx context.Context, req *LookupRepoRequest) (*LookupRepoResponse, error) {
	var resp LookupRepoResponse
	if err := c.post(ctx, "/v1/repos/lookup", req, &resp); err != nil {
		// Return nil for 404 (not found)
		if clientErr, ok := err.(*Error); ok && clientErr.StatusCode == 404 {
			return nil, nil
		}
		return nil, err
	}
	return &resp, nil
}

// RefreshPresenceRequest is the request body for /v1/agents/register (presence refresh).
type RefreshPresenceRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Alias       string `json:"alias"`
	HumanName   string `json:"human_name,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
	RepoID      string `json:"repo_id,omitempty"`
	RepoOrigin  string `json:"repo_origin,omitempty"`
	// CanonicalOrigin is a normalized repo identifier like "github.com/org/repo".
	CanonicalOrigin string `json:"canonical_origin,omitempty"`
	Hostname        string `json:"hostname,omitempty"`
	WorkspacePath   string `json:"workspace_path,omitempty"`
	Program         string `json:"program,omitempty"`
	Model           string `json:"model,omitempty"`
	Repo            string `json:"repo,omitempty"`
	Branch          string `json:"branch,omitempty"`
	Role            string `json:"role,omitempty"`
	TTLSeconds      int    `json:"ttl_seconds,omitempty"`
}

// RefreshPresenceResponse is the response from /v1/agents/register.
type RefreshPresenceResponse struct {
	Agent     map[string]any `json:"agent"`
	Workspace map[string]any `json:"workspace"`
}

// RefreshPresence refreshes the agent's presence in BeadHub.
// This is called by all bdh commands to keep presence alive.
func (c *Client) RefreshPresence(ctx context.Context, req *RefreshPresenceRequest) (*RefreshPresenceResponse, error) {
	var resp RefreshPresenceResponse
	if err := c.post(ctx, "/v1/agents/register", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RegisterWorkspaceRequest is the request body for /v1/workspaces/register.
type RegisterWorkspaceRequest struct {
	RepoOrigin    string `json:"repo_origin"`
	Role          string `json:"role,omitempty"`
	Hostname      string `json:"hostname,omitempty"`
	WorkspacePath string `json:"workspace_path,omitempty"`
}

// RegisterWorkspaceResponse is the response from /v1/workspaces/register.
type RegisterWorkspaceResponse struct {
	WorkspaceID     string `json:"workspace_id"`
	ProjectID       string `json:"project_id"`
	ProjectSlug     string `json:"project_slug"`
	RepoID          string `json:"repo_id"`
	CanonicalOrigin string `json:"canonical_origin"`
	Alias           string `json:"alias"`
	HumanName       string `json:"human_name"`
	Created         bool   `json:"created"`
}

// RegisterWorkspace registers a workspace with the server.
// Returns error with status 409 if alias is already taken.
func (c *Client) RegisterWorkspace(ctx context.Context, req *RegisterWorkspaceRequest) (*RegisterWorkspaceResponse, error) {
	var resp RegisterWorkspaceResponse
	if err := c.post(ctx, "/v1/workspaces/register", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SuggestNamePrefixRequest is the request body for /v1/workspaces/suggest-name-prefix.
type SuggestNamePrefixRequest struct {
	OriginURL string `json:"origin_url"`
}

// SuggestNamePrefixResponse is the response from /v1/workspaces/suggest-name-prefix.
type SuggestNamePrefixResponse struct {
	NamePrefix      string `json:"name_prefix"`
	ProjectID       string `json:"project_id"`
	ProjectSlug     string `json:"project_slug"`
	RepoID          string `json:"repo_id"`
	CanonicalOrigin string `json:"canonical_origin"`
}

// SuggestNamePrefix gets the next available name prefix for a new workspace.
// Returns 404 if repo is not registered, 409 if repo exists in multiple projects.
func (c *Client) SuggestNamePrefix(ctx context.Context, req *SuggestNamePrefixRequest) (*SuggestNamePrefixResponse, error) {
	var resp SuggestNamePrefixResponse
	if err := c.post(ctx, "/v1/workspaces/suggest-name-prefix", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// InboxRequest is the request parameters for GET /v1/messages/inbox.
type InboxRequest struct {
	WorkspaceID   string
	Limit         int
	UnreadOnly    bool
	FromWorkspace string // Filter to messages from this workspace
	FromAlias     string // Filter to messages from sender with this alias
}

// InboxResponse is the response from GET /v1/messages/inbox.
type InboxResponse struct {
	Messages []Message `json:"messages"`
	Count    int       `json:"count"`
	HasMore  bool      `json:"has_more"`
}

// Message represents a message in the inbox.
type Message struct {
	MessageID     string `json:"message_id"`
	FromWorkspace string `json:"from_workspace"`
	FromAlias     string `json:"from_alias"`
	Subject       string `json:"subject"`
	Body          string `json:"body"`
	Priority      string `json:"priority"`
	ThreadID      string `json:"thread_id,omitempty"`
	Read          bool   `json:"read"`
	CreatedAt     string `json:"created_at"`
}

// Inbox fetches messages from the workspace's inbox.
func (c *Client) Inbox(ctx context.Context, req *InboxRequest) (*InboxResponse, error) {
	var resp InboxResponse
	if err := c.get(ctx, "/v1/messages/inbox", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AckRequest is the request body for POST /v1/messages/{id}/ack.
type AckRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

// AckResponse is the response from POST /v1/messages/{id}/ack.
type AckResponse struct {
	MessageID      string `json:"message_id"`
	AcknowledgedAt string `json:"acknowledged_at"`
}

// Ack acknowledges (marks as read) a message.
func (c *Client) Ack(ctx context.Context, messageID string, req *AckRequest) (*AckResponse, error) {
	var resp AckResponse
	path := fmt.Sprintf("/v1/messages/%s/ack", url.PathEscape(messageID))
	if err := c.post(ctx, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendRequest is the request body for POST /v1/messages.
type SendRequest struct {
	FromWorkspace string `json:"from_workspace"`
	ToWorkspace   string `json:"to_workspace"`
	FromAlias     string `json:"from_alias"`
	Subject       string `json:"subject,omitempty"`
	Body          string `json:"body"`
	Priority      string `json:"priority,omitempty"`
}

// SendResponse is the response from POST /v1/messages.
type SendResponse struct {
	MessageID   string `json:"message_id"`
	Status      string `json:"status"`
	DeliveredAt string `json:"delivered_at"`
}

// Send sends a message to another workspace.
func (c *Client) Send(ctx context.Context, req *SendRequest) (*SendResponse, error) {
	var resp SendResponse
	if err := c.post(ctx, "/v1/messages", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkspacesRequest is the request parameters for GET /v1/workspaces.
type WorkspacesRequest struct {
	HumanName       string
	Repo            string
	Alias           string
	Hostname        string
	IncludeClaims   bool
	IncludePresence *bool
	IncludeDeleted  bool
	Limit           int
}

// TeamWorkspacesRequest is the request parameters for GET /v1/workspaces/team.
type TeamWorkspacesRequest struct {
	HumanName                string
	Repo                     string
	IncludeClaims            *bool
	IncludePresence          *bool
	OnlyWithClaims           *bool
	AlwaysIncludeWorkspaceID string
	Limit                    int
}

// WorkspacesResponse is the response from GET /v1/workspaces.
type WorkspacesResponse struct {
	Workspaces []Workspace `json:"workspaces"`
	Count      int         `json:"count"`
}

// Claim represents an active bead claim by a workspace.
type Claim struct {
	BeadID    string `json:"bead_id"`
	Title     string `json:"title,omitempty"`
	ClaimedAt string `json:"claimed_at"`
	ApexID    string `json:"apex_id,omitempty"`
	ApexTitle string `json:"apex_title,omitempty"`
	ApexType  string `json:"apex_type,omitempty"`
}

// Workspace represents workspace presence information.
type Workspace struct {
	WorkspaceID       string  `json:"workspace_id"`
	Alias             string  `json:"alias"`
	HumanName         string  `json:"human_name"`
	ProjectSlug       string  `json:"project_slug"`
	Role              string  `json:"role,omitempty"`
	Hostname          string  `json:"hostname,omitempty"`
	WorkspacePath     string  `json:"workspace_path,omitempty"`
	ApexID            string  `json:"apex_id,omitempty"`
	ApexTitle         string  `json:"apex_title,omitempty"`
	ApexType          string  `json:"apex_type,omitempty"`
	FocusApexID       string  `json:"focus_apex_id,omitempty"`
	FocusApexTitle    string  `json:"focus_apex_title,omitempty"`
	FocusApexType     string  `json:"focus_apex_type,omitempty"`
	FocusApexRepoName string  `json:"focus_apex_repo_name,omitempty"`
	FocusApexBranch   string  `json:"focus_apex_branch,omitempty"`
	FocusUpdatedAt    string  `json:"focus_updated_at,omitempty"`
	Status            string  `json:"status"`
	LastSeen          string  `json:"last_seen"`
	Claims            []Claim `json:"claims"`
}

// DeleteWorkspaceResponse is the response from DELETE /v1/workspaces/{id}.
type DeleteWorkspaceResponse struct {
	WorkspaceID string `json:"workspace_id"`
	Alias       string `json:"alias"`
	DeletedAt   string `json:"deleted_at"`
}

// DeleteWorkspace soft-deletes a workspace by its ID.
// Returns nil error and nil response if workspace was already deleted (404).
func (c *Client) DeleteWorkspace(ctx context.Context, workspaceID string) (*DeleteWorkspaceResponse, error) {
	var resp DeleteWorkspaceResponse
	if err := c.delete(ctx, "/v1/workspaces/"+url.PathEscape(workspaceID), &resp); err != nil {
		// Return nil for 404 (already deleted or doesn't exist)
		if clientErr, ok := err.(*Error); ok && clientErr.StatusCode == 404 {
			return nil, nil
		}
		return nil, err
	}
	return &resp, nil
}

// Workspaces lists workspaces from the BeadHub server.
func (c *Client) Workspaces(ctx context.Context, req *WorkspacesRequest) (*WorkspacesResponse, error) {
	var resp WorkspacesResponse
	if err := c.get(ctx, "/v1/workspaces", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TeamWorkspaces lists a bounded team-status view from the BeadHub server.
func (c *Client) TeamWorkspaces(ctx context.Context, req *TeamWorkspacesRequest) (*WorkspacesResponse, error) {
	var resp WorkspacesResponse
	if err := c.get(ctx, "/v1/workspaces/team", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// =============================================================================
// Project Policy API
// =============================================================================

// ActivePolicyRequest is the request parameters for GET /v1/policies/active.
type ActivePolicyRequest struct {
	Role         string
	OnlySelected bool
}

// PolicyInvariant represents a single global invariant.
type PolicyInvariant struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	BodyMD string `json:"body_md"`
}

// PolicyRolePlaybook represents a role playbook within a policy bundle.
type PolicyRolePlaybook struct {
	Title      string `json:"title"`
	PlaybookMD string `json:"playbook_md"`
}

// SelectedPolicyRole represents the selected role playbook returned by the server.
type SelectedPolicyRole struct {
	Role       string `json:"role"`
	Title      string `json:"title"`
	PlaybookMD string `json:"playbook_md"`
}

// ActivePolicyResponse is the response from GET /v1/policies/active.
type ActivePolicyResponse struct {
	PolicyID     string                        `json:"policy_id"`
	ProjectID    string                        `json:"project_id"`
	Version      int                           `json:"version"`
	UpdatedAt    string                        `json:"updated_at"`
	Invariants   []PolicyInvariant             `json:"invariants"`
	Roles        map[string]PolicyRolePlaybook `json:"roles,omitempty"`
	SelectedRole *SelectedPolicyRole           `json:"selected_role,omitempty"`
	Adapters     map[string]any                `json:"adapters,omitempty"`
}

// ActivePolicy fetches the active policy bundle for a project.
func (c *Client) ActivePolicy(ctx context.Context, req *ActivePolicyRequest) (*ActivePolicyResponse, error) {
	var resp ActivePolicyResponse
	if err := c.get(ctx, "/v1/policies/active", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ActivePolicyFetchOptions controls conditional GET behavior for ActivePolicyFetch.
type ActivePolicyFetchOptions struct {
	IfNoneMatch     string
	IfModifiedSince string
}

// ActivePolicyFetchResponse includes response metadata used for caching.
type ActivePolicyFetchResponse struct {
	StatusCode   int
	ETag         string
	LastModified string
	Policy       *ActivePolicyResponse
}

// ActivePolicyFetch fetches the active policy bundle with optional conditional GET headers.
// Returns StatusCode=304 with a nil Policy when the server reports "Not Modified".
func (c *Client) ActivePolicyFetch(ctx context.Context, reqParams *ActivePolicyRequest, opts *ActivePolicyFetchOptions) (*ActivePolicyFetchResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/policies/active", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if opts != nil {
		if opts.IfNoneMatch != "" {
			req.Header.Set("If-None-Match", opts.IfNoneMatch)
		}
		if opts.IfModifiedSince != "" {
			req.Header.Set("If-Modified-Since", opts.IfModifiedSince)
		}
	}

	if reqParams != nil {
		q := req.URL.Query()
		if reqParams.Role != "" {
			q.Set("role", reqParams.Role)
		}
		if reqParams.OnlySelected {
			q.Set("only_selected", "true")
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if int64(len(respBodyBytes)) > maxResponseSize {
		return nil, fmt.Errorf("response exceeds maximum size of %d bytes", maxResponseSize)
	}

	meta := &ActivePolicyFetchResponse{
		StatusCode:   resp.StatusCode,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}

	if resp.StatusCode == http.StatusNotModified {
		return meta, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &Error{
			StatusCode: resp.StatusCode,
			Body:       string(respBodyBytes),
		}
	}

	var policy ActivePolicyResponse
	if err := json.Unmarshal(respBodyBytes, &policy); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	meta.Policy = &policy
	return meta, nil
}

// ResetPolicyResponse is the response from POST /v1/policies/reset.
type ResetPolicyResponse struct {
	Reset          bool   `json:"reset"`
	ActivePolicyID string `json:"active_policy_id"`
	Version        int    `json:"version"`
}

// ResetPolicy resets the project's policy to the default bundle.
// Creates a new policy version and activates it.
func (c *Client) ResetPolicy(ctx context.Context) (*ResetPolicyResponse, error) {
	var resp ResetPolicyResponse
	if err := c.post(ctx, "/v1/policies/reset", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// StatusRequest is the request parameters for GET /v1/status.
type StatusRequest struct {
	WorkspaceID string
	Repo        string
}

// StatusResponse is the response from GET /v1/status.
type StatusResponse struct {
	Workspace          map[string]any `json:"workspace"`
	Agents             []StatusAgent  `json:"agents"`
	Locks              []any          `json:"locks"`
	EscalationsPending int            `json:"escalations_pending"`
	Timestamp          string         `json:"timestamp"`
}

// StatusAgent represents an agent in the status response.
type StatusAgent struct {
	Alias        string   `json:"alias"`
	Member       string   `json:"member"`
	Program      string   `json:"program"`
	Role         string   `json:"role,omitempty"`
	Status       string   `json:"status"`
	CurrentLocks []string `json:"current_locks"`
	CurrentIssue string   `json:"current_issue"`
	LastSeen     string   `json:"last_seen"`
}

// Status fetches coordination status from the BeadHub server.
func (c *Client) Status(ctx context.Context, req *StatusRequest) (*StatusResponse, error) {
	var resp StatusResponse
	if err := c.get(ctx, "/v1/status", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// EscalateRequest is the request body for POST /v1/escalations.
type EscalateRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Alias       string `json:"alias"`
	Subject     string `json:"subject"`
	Situation   string `json:"situation"`
}

// EscalateResponse is the response from POST /v1/escalations.
type EscalateResponse struct {
	EscalationID string `json:"escalation_id"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
	ExpiresAt    string `json:"expires_at,omitempty"`
}

// Escalate creates an escalation to a human.
func (c *Client) Escalate(ctx context.Context, req *EscalateRequest) (*EscalateResponse, error) {
	var resp EscalateResponse
	if err := c.post(ctx, "/v1/escalations", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// =============================================================================
// Chat API (real-time agent chat sessions)
// =============================================================================

// CreateChatSessionRequest is the request body for POST /v1/chat/sessions.
// v2.1 API: uses from_workspace/from_alias/to_aliases instead of initiator/target.
type CreateChatSessionRequest struct {
	FromWorkspace string   `json:"from_workspace"`
	FromAlias     string   `json:"from_alias"`
	ToAliases     []string `json:"to_aliases"`
	Message       string   `json:"message"`
	Leaving       bool     `json:"leaving,omitempty"` // Signals sender is leaving the conversation
}

// ChatParticipant represents a participant in a chat session.
type ChatParticipant struct {
	WorkspaceID string `json:"workspace_id"`
	Alias       string `json:"alias"`
}

// CreateChatSessionResponse is the response from POST /v1/chat/sessions.
// v2.5 API: returns participants list, message_id, targets_connected, and targets_left.
type CreateChatSessionResponse struct {
	SessionID        string            `json:"session_id"`
	MessageID        string            `json:"message_id"`
	Participants     []ChatParticipant `json:"participants"`
	SSEURL           string            `json:"sse_url"`
	TargetsConnected []string          `json:"targets_connected"` // Aliases of targets currently connected to SSE (informational)
	TargetsLeft      []string          `json:"targets_left"`      // Aliases of targets whose last message had sender_leaving=true
}

// CreateChatSession creates or finds a chat session and sends a message.
func (c *Client) CreateChatSession(ctx context.Context, req *CreateChatSessionRequest) (*CreateChatSessionResponse, error) {
	var resp CreateChatSessionResponse
	if err := c.post(ctx, "/v1/chat/sessions", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ChatMessage represents a message in a chat session.
type ChatMessage struct {
	MessageID     string `json:"message_id"`
	FromAgent     string `json:"from_agent"`
	Body          string `json:"body"`
	CreatedAt     string `json:"created_at"`
	SenderLeaving bool   `json:"sender_leaving,omitempty"` // True when sender left the conversation
}

// SendChatMessageRequest is the request body for POST /v1/chat/sessions/{id}/messages.
type SendChatMessageRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Alias       string `json:"alias"`
	Body        string `json:"body"`
	HangOn      bool   `json:"hang_on,omitempty"` // Request more time to reply
}

// SendChatMessageResponse is the response from POST /v1/chat/sessions/{id}/messages.
type SendChatMessageResponse struct {
	MessageID          string `json:"message_id"`
	Delivered          bool   `json:"delivered"`
	ExtendsWaitSeconds int    `json:"extends_wait_seconds"` // How long wait is extended (for hang_on)
}

// SendChatMessage sends a message to a chat session.
func (c *Client) SendChatMessage(ctx context.Context, sessionID string, req *SendChatMessageRequest) (*SendChatMessageResponse, error) {
	var resp SendChatMessageResponse
	path := fmt.Sprintf("/v1/chat/sessions/%s/messages", url.PathEscape(sessionID))
	if err := c.post(ctx, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// MarkReadRequest is the request body for POST /v1/chat/sessions/{id}/read.
type MarkReadRequest struct {
	WorkspaceID   string `json:"workspace_id"`
	UpToMessageID string `json:"up_to_message_id"`
}

// MarkReadResponse is the response from POST /v1/chat/sessions/{id}/read.
type MarkReadResponse struct {
	Success             bool `json:"success"`
	MessagesMarked      int  `json:"messages_marked"`       // Number of messages actually marked as read
	WaitExtendedSeconds int  `json:"wait_extended_seconds"` // If sender was waiting, their timeout was extended by this
}

// MarkRead marks messages as read up to a given message ID.
func (c *Client) MarkRead(ctx context.Context, sessionID string, req *MarkReadRequest) (*MarkReadResponse, error) {
	var resp MarkReadResponse
	path := fmt.Sprintf("/v1/chat/sessions/%s/read", url.PathEscape(sessionID))
	if err := c.post(ctx, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetPendingChatsRequest is the request parameters for GET /v1/chat/pending.
type GetPendingChatsRequest struct {
	WorkspaceID string
}

// PendingConversation represents a conversation with unread messages.
// v2.1 API: uses participants/last_message/unread_count instead of from_alias/message/expires.
type PendingConversation struct {
	SessionID            string   `json:"session_id"`
	Participants         []string `json:"participants"`
	LastMessage          string   `json:"last_message"`
	LastFrom             string   `json:"last_from"`
	UnreadCount          int      `json:"unread_count"`
	LastActivity         string   `json:"last_activity"`
	SenderWaiting        bool     `json:"sender_waiting"`         // True if last sender is still connected (waiting for reply)
	TimeRemainingSeconds *int     `json:"time_remaining_seconds"` // Seconds until sender's deadline (nil if no deadline)
}

// GetPendingChatsResponse is the response from GET /v1/chat/pending.
type GetPendingChatsResponse struct {
	Pending         []PendingConversation `json:"pending"`
	MessagesWaiting int                   `json:"messages_waiting"`
}

// GetPendingChats gets conversations with unread messages for a workspace.
func (c *Client) GetPendingChats(ctx context.Context, req *GetPendingChatsRequest) (*GetPendingChatsResponse, error) {
	var resp GetPendingChatsResponse
	if err := c.get(ctx, "/v1/chat/pending", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetMessagesRequest is the request parameters for GET /v1/chat/sessions/{id}/messages.
type GetMessagesRequest struct {
	WorkspaceID string
	Since       string
	UnreadOnly  bool
	Limit       int
}

// GetMessagesResponse is the response from GET /v1/chat/sessions/{id}/messages.
type GetMessagesResponse struct {
	SessionID string        `json:"session_id"`
	Messages  []ChatMessage `json:"messages"`
}

// GetMessages gets message history for a chat session.
func (c *Client) GetMessages(ctx context.Context, sessionID string, req *GetMessagesRequest) (*GetMessagesResponse, error) {
	var resp GetMessagesResponse
	path := fmt.Sprintf("/v1/chat/sessions/%s/messages", url.PathEscape(sessionID))
	if err := c.get(ctx, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListChatSessionsRequest is the request parameters for GET /v1/chat/sessions.
type ListChatSessionsRequest struct {
	WorkspaceID string
}

// ChatSessionItem represents a chat session in the session list.
type ChatSessionItem struct {
	SessionID      string `json:"session_id"`
	InitiatorAgent string `json:"initiator_agent"`
	TargetAgent    string `json:"target_agent"`
	CreatedAt      string `json:"created_at"`
}

// ListChatSessionsResponse is the response from GET /v1/chat/sessions.
type ListChatSessionsResponse struct {
	Sessions []ChatSessionItem `json:"sessions"`
}

// ListChatSessions lists chat sessions for a workspace.
func (c *Client) ListChatSessions(ctx context.Context, req *ListChatSessionsRequest) (*ListChatSessionsResponse, error) {
	var resp ListChatSessionsResponse
	if err := c.get(ctx, "/v1/chat/sessions", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// =============================================================================
// Reservations API (file reservations)
// =============================================================================

// LockRequest is the request body for POST /v1/reservations.
type LockRequest struct {
	WorkspaceID string   `json:"workspace_id"`
	Alias       string   `json:"alias"`
	Paths       []string `json:"paths"`
	TTLSeconds  int      `json:"ttl_seconds,omitempty"`
	Exclusive   bool     `json:"exclusive"`
	Reason      string   `json:"reason,omitempty"`
	BeadID      string   `json:"bead_id,omitempty"`
}

// GrantedLock represents a successfully acquired reservation.
type GrantedLock struct {
	ReservationID string `json:"reservation_id"`
	Path          string `json:"path"`
	ExpiresAt     string `json:"expires_at"`
}

// ConflictLock represents a conflicting reservation held by another agent.
type ConflictLock struct {
	Path              string  `json:"path"`
	HeldBy            string  `json:"held_by"`
	WorkspaceID       string  `json:"workspace_id"`
	BeadID            *string `json:"bead_id,omitempty"`
	Reason            *string `json:"reason,omitempty"`
	Exclusive         bool    `json:"exclusive"`
	AcquiredAt        *string `json:"acquired_at,omitempty"`
	ExpiresAt         *string `json:"expires_at,omitempty"`
	RetryAfterSeconds int     `json:"retry_after_seconds"`
}

// LockResponse is the response from POST /v1/reservations.
type LockResponse struct {
	Granted   []GrantedLock  `json:"granted"`
	Conflicts []ConflictLock `json:"conflicts"`
}

// Lock acquires file reservations.
func (c *Client) Lock(ctx context.Context, req *LockRequest) (*LockResponse, error) {
	var resp LockResponse
	if err := c.post(ctx, "/v1/reservations", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UnlockRequest is the request body for POST /v1/reservations/release.
type UnlockRequest struct {
	WorkspaceID string   `json:"workspace_id"`
	Alias       string   `json:"alias"`
	Paths       []string `json:"paths"`
}

// UnlockResponse is the response from POST /v1/reservations/release.
type UnlockResponse struct {
	Released []string `json:"released"`
	NotFound []string `json:"not_found"`
	NotOwner []string `json:"not_owner"`
}

// Unlock releases file reservations.
func (c *Client) Unlock(ctx context.Context, req *UnlockRequest) (*UnlockResponse, error) {
	var resp UnlockResponse
	if err := c.post(ctx, "/v1/reservations/release", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListLocksRequest is the request parameters for GET /v1/reservations.
type ListLocksRequest struct {
	WorkspaceID string
	Alias       string
	PathPrefix  string
}

// LockInfo represents a reservation in the list response.
type LockInfo struct {
	ReservationID       string  `json:"reservation_id"`
	Path                string  `json:"path"`
	Alias               string  `json:"alias"`
	WorkspaceID         string  `json:"workspace_id"`
	ProjectID           string  `json:"project_id"`
	BeadID              *string `json:"bead_id,omitempty"`
	Reason              *string `json:"reason,omitempty"`
	Exclusive           bool    `json:"exclusive"`
	AcquiredAt          string  `json:"acquired_at"`
	ExpiresAt           string  `json:"expires_at"`
	TTLRemainingSeconds int     `json:"ttl_remaining_seconds"`
}

// ListLocksResponse is the response from GET /v1/reservations.
type ListLocksResponse struct {
	Reservations []LockInfo `json:"reservations"`
	Count        int        `json:"count"`
}

// ListLocks lists active file reservations.
func (c *Client) ListLocks(ctx context.Context, req *ListLocksRequest) (*ListLocksResponse, error) {
	var resp ListLocksResponse
	if err := c.get(ctx, "/v1/reservations", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Error represents an error response from the BeadHub server.
type Error struct {
	StatusCode int
	Body       string
}

func (e *Error) Error() string {
	return fmt.Sprintf("BeadHub error (status %d): %s", e.StatusCode, e.Body)
}

// post sends a POST request and decodes the JSON response.
func (c *Client) post(ctx context.Context, path string, reqBody, respBody any) error {
	return c.postWithHeaders(ctx, path, reqBody, respBody, nil)
}

// postWithHeaders sends a POST request with additional headers and decodes the JSON response.
func (c *Client) postWithHeaders(ctx context.Context, path string, reqBody, respBody any, headers map[string]string) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read maxResponseSize+1 to detect oversized responses while still accepting
	// responses exactly at the limit. If we read more than maxResponseSize, reject.
	respBodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if int64(len(respBodyBytes)) > maxResponseSize {
		return fmt.Errorf("response exceeds maximum size of %d bytes", maxResponseSize)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{
			StatusCode: resp.StatusCode,
			Body:       string(respBodyBytes),
		}
	}

	if err := json.Unmarshal(respBodyBytes, respBody); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	return nil
}

// delete sends a DELETE request and decodes the JSON response.
func (c *Client) delete(ctx context.Context, path string, respBody any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	// API key auth: use Authorization header
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if int64(len(respBodyBytes)) > maxResponseSize {
		return fmt.Errorf("response exceeds maximum size of %d bytes", maxResponseSize)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{
			StatusCode: resp.StatusCode,
			Body:       string(respBodyBytes),
		}
	}

	if err := json.Unmarshal(respBodyBytes, respBody); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	return nil
}

// get sends a GET request with query parameters and decodes the JSON response.
func (c *Client) get(ctx context.Context, path string, params any, respBody any) error {
	return c.getWithHeaders(ctx, path, params, respBody, nil)
}

// getWithHeaders sends a GET request with query parameters, sets optional headers, and decodes the JSON response.
func (c *Client) getWithHeaders(ctx context.Context, path string, params any, respBody any, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Build query parameters from the request struct
	if params != nil {
		// Avoid panics when params is a typed-nil pointer stored in an interface.
		v := reflect.ValueOf(params)
		if v.Kind() == reflect.Ptr && v.IsNil() {
			params = nil
		}
	}
	if params != nil {
		q := req.URL.Query()
		switch p := params.(type) {
		case *InboxRequest:
			q.Set("workspace_id", p.WorkspaceID)
			if p.Limit > 0 {
				q.Set("limit", fmt.Sprintf("%d", p.Limit))
			}
			q.Set("unread_only", fmt.Sprintf("%t", p.UnreadOnly))
			if p.FromWorkspace != "" {
				q.Set("from_workspace", p.FromWorkspace)
			}
			if p.FromAlias != "" {
				q.Set("from_alias", p.FromAlias)
			}
		case *WorkspacesRequest:
			if p.HumanName != "" {
				q.Set("human_name", p.HumanName)
			}
			if p.Repo != "" {
				q.Set("repo", p.Repo)
			}
			if p.Alias != "" {
				q.Set("alias", p.Alias)
			}
			if p.Hostname != "" {
				q.Set("hostname", p.Hostname)
			}
			if p.IncludeClaims {
				q.Set("include_claims", "true")
			}
			if p.IncludePresence != nil {
				q.Set("include_presence", fmt.Sprintf("%t", *p.IncludePresence))
			}
			if p.IncludeDeleted {
				q.Set("include_deleted", "true")
			}
			if p.Limit > 0 {
				q.Set("limit", fmt.Sprintf("%d", p.Limit))
			}
		case *TeamWorkspacesRequest:
			if p.HumanName != "" {
				q.Set("human_name", p.HumanName)
			}
			if p.Repo != "" {
				q.Set("repo", p.Repo)
			}
			if p.IncludeClaims != nil {
				q.Set("include_claims", fmt.Sprintf("%t", *p.IncludeClaims))
			}
			if p.IncludePresence != nil {
				q.Set("include_presence", fmt.Sprintf("%t", *p.IncludePresence))
			}
			if p.OnlyWithClaims != nil {
				q.Set("only_with_claims", fmt.Sprintf("%t", *p.OnlyWithClaims))
			}
			if p.AlwaysIncludeWorkspaceID != "" {
				q.Set("always_include_workspace_id", p.AlwaysIncludeWorkspaceID)
			}
			if p.Limit > 0 {
				q.Set("limit", fmt.Sprintf("%d", p.Limit))
			}
		case *StatusRequest:
			if p.WorkspaceID != "" {
				q.Set("workspace_id", p.WorkspaceID)
			}
			if p.Repo != "" {
				q.Set("repo", p.Repo)
			}
		case *ListChatSessionsRequest:
			if p.WorkspaceID != "" {
				q.Set("workspace_id", p.WorkspaceID)
			}
		case *GetPendingChatsRequest:
			if p.WorkspaceID != "" {
				q.Set("workspace_id", p.WorkspaceID)
			}
		case *GetMessagesRequest:
			if p.WorkspaceID != "" {
				q.Set("workspace_id", p.WorkspaceID)
			}
			if p.Since != "" {
				q.Set("since", p.Since)
			}
			if p.UnreadOnly {
				q.Set("unread_only", "true")
			}
			if p.Limit > 0 {
				q.Set("limit", fmt.Sprintf("%d", p.Limit))
			}
		case *ListLocksRequest:
			if p.WorkspaceID != "" {
				q.Set("workspace_id", p.WorkspaceID)
			}
			if p.Alias != "" {
				q.Set("alias", p.Alias)
			}
			if p.PathPrefix != "" {
				q.Set("path_prefix", p.PathPrefix)
			}
		case *ActivePolicyRequest:
			if p.Role != "" {
				q.Set("role", p.Role)
			}
			if p.OnlySelected {
				q.Set("only_selected", "true")
			}
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read maxResponseSize+1 to detect oversized responses while still accepting
	// responses exactly at the limit. If we read more than maxResponseSize, reject.
	respBodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if int64(len(respBodyBytes)) > maxResponseSize {
		return fmt.Errorf("response exceeds maximum size of %d bytes", maxResponseSize)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{
			StatusCode: resp.StatusCode,
			Body:       string(respBodyBytes),
		}
	}

	if err := json.Unmarshal(respBodyBytes, respBody); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	return nil
}
