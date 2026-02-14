package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

// --- Shared helpers ---

// slugifyInvariantID converts a title to a slug: lowercase, non-alphanumeric → hyphens, collapsed, trimmed.
func slugifyInvariantID(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// readContentFlag returns the value as-is, unless it is "-", in which case it reads stdin.
func readContentFlag(value string) (string, error) {
	if value != "-" {
		return value, nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}
	return string(data), nil
}

// invalidatePolicyCache deletes all policy cache files from .beadhub-cache/.
func invalidatePolicyCache(workspaceRoot string) {
	cacheDir := filepath.Join(workspaceRoot, ".beadhub-cache")
	matches, err := filepath.Glob(filepath.Join(cacheDir, "policy-active*.json"))
	if err != nil {
		return
	}
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// mutateActivePolicy performs a read-modify-write cycle on the active policy bundle:
// 1. Fetch the active policy (full, only_selected=false)
// 2. Apply mutateFn to modify the bundle in memory
// 3. POST the modified bundle as a new policy version (with base_policy_id)
// 4. Activate the new version
// 5. Invalidate the local policy cache
func mutateActivePolicy(cfg *config.Config, mutateFn func(*client.PolicyBundle, *client.ActivePolicyResponse) error) error {
	c, err := newBeadHubClientRequired(cfg.BeadhubURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	onlySelected := false
	active, err := c.ActivePolicy(ctx, &client.ActivePolicyRequest{
		OnlySelected: &onlySelected,
	})
	if err != nil {
		return fmt.Errorf("fetching active policy: %w", err)
	}

	bundle := client.PolicyBundle{
		Invariants: active.Invariants,
		Roles:      active.Roles,
		Adapters:   active.Adapters,
	}
	if bundle.Roles == nil {
		bundle.Roles = make(map[string]client.PolicyRolePlaybook)
	}
	if bundle.Invariants == nil {
		bundle.Invariants = []client.PolicyInvariant{}
	}

	if err := mutateFn(&bundle, active); err != nil {
		return err
	}

	createResp, err := c.CreatePolicy(ctx, &client.CreatePolicyRequest{
		Bundle:       bundle,
		BasePolicyID: active.PolicyID,
	})
	if err != nil {
		return fmt.Errorf("creating policy version: %w", err)
	}

	if _, err := c.ActivatePolicy(ctx, createResp.PolicyID); err != nil {
		return fmt.Errorf("activating policy version: %w", err)
	}

	workspaceRoot := filepath.Dir(config.GetPath())
	if root, err := config.WorkspaceRoot(); err == nil {
		workspaceRoot = root
	}
	invalidatePolicyCache(workspaceRoot)

	return nil
}

// --- :policy add ---

var (
	policyAddInvariant bool
	policyAddRole      bool
	policyAddTitle     string
	policyAddName      string
	policyAddGuidance  string
	policyAddPlaybook  string
)

var policyAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add an invariant or role to the active policy",
	Long: `Add a new invariant or role playbook to the active project policy.

Examples:
  bdh :policy add --invariant --title="No TODO lists" --guidance="Use beads."
  bdh :policy add --role --name=reviewer --playbook="Review all PRs."
  echo "long text" | bdh :policy add --invariant --title="Long rule" --guidance=-`,
	RunE: runPolicyAdd,
}

func init() {
	policyAddCmd.Flags().BoolVar(&policyAddInvariant, "invariant", false, "Add an invariant")
	policyAddCmd.Flags().BoolVar(&policyAddRole, "role", false, "Add a role playbook")
	policyAddCmd.Flags().StringVar(&policyAddTitle, "title", "", "Invariant title")
	policyAddCmd.Flags().StringVar(&policyAddName, "name", "", "Role name")
	policyAddCmd.Flags().StringVar(&policyAddGuidance, "guidance", "", "Invariant guidance (body markdown); use '-' for stdin")
	policyAddCmd.Flags().StringVar(&policyAddPlaybook, "playbook", "", "Role playbook markdown; use '-' for stdin")
}

func runPolicyAdd(cmd *cobra.Command, args []string) error {
	if policyAddInvariant == policyAddRole {
		return fmt.Errorf("exactly one of --invariant or --role is required")
	}

	cfg, err := loadAndValidateConfig()
	if err != nil {
		return err
	}

	if policyAddInvariant {
		return addInvariant(cfg)
	}
	return addRole(cfg)
}

func addInvariant(cfg *config.Config) error {
	if policyAddTitle == "" {
		return fmt.Errorf("--title is required for --invariant")
	}
	guidance, err := readContentFlag(policyAddGuidance)
	if err != nil {
		return err
	}
	if strings.TrimSpace(guidance) == "" {
		return fmt.Errorf("--guidance is required for --invariant")
	}

	id := slugifyInvariantID(policyAddTitle)
	if id == "" {
		return fmt.Errorf("title %q produces an empty ID", policyAddTitle)
	}

	return mutateActivePolicy(cfg, func(bundle *client.PolicyBundle, _ *client.ActivePolicyResponse) error {
		for _, inv := range bundle.Invariants {
			if inv.ID == id {
				return fmt.Errorf("invariant %q already exists", id)
			}
		}
		bundle.Invariants = append(bundle.Invariants, client.PolicyInvariant{
			ID:     id,
			Title:  policyAddTitle,
			BodyMD: guidance,
		})
		fmt.Fprintf(os.Stderr, "Added invariant %q\n", id)
		return nil
	})
}

func addRole(cfg *config.Config) error {
	if policyAddName == "" {
		return fmt.Errorf("--name is required for --role")
	}
	name := config.NormalizeRole(policyAddName)
	if name == "" || !config.IsValidRole(name) {
		return fmt.Errorf("invalid role name %q", policyAddName)
	}

	playbook, err := readContentFlag(policyAddPlaybook)
	if err != nil {
		return err
	}
	if strings.TrimSpace(playbook) == "" {
		return fmt.Errorf("--playbook is required for --role")
	}

	return mutateActivePolicy(cfg, func(bundle *client.PolicyBundle, _ *client.ActivePolicyResponse) error {
		if _, exists := bundle.Roles[name]; exists {
			return fmt.Errorf("role %q already exists", name)
		}
		bundle.Roles[name] = client.PolicyRolePlaybook{
			Title:      capitalizeWords(name),
			PlaybookMD: playbook,
		}
		fmt.Fprintf(os.Stderr, "Added role %q\n", name)
		return nil
	})
}

// --- :policy edit ---

var (
	policyEditInvariant string
	policyEditRole      string
	policyEditGuidance  string
	policyEditPlaybook  string
	policyEditTitle     string
)

var policyEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit an invariant or role in the active policy",
	Long: `Edit an existing invariant or role playbook in the active project policy.

Examples:
  bdh :policy edit --invariant my-rule --guidance="Updated."
  bdh :policy edit --invariant my-rule --title="New Title" --guidance="Updated."
  bdh :policy edit --role reviewer --playbook="Updated playbook."`,
	RunE: runPolicyEdit,
}

func init() {
	policyEditCmd.Flags().StringVar(&policyEditInvariant, "invariant", "", "Invariant ID to edit")
	policyEditCmd.Flags().StringVar(&policyEditRole, "role", "", "Role name to edit")
	policyEditCmd.Flags().StringVar(&policyEditGuidance, "guidance", "", "Updated invariant guidance; use '-' for stdin")
	policyEditCmd.Flags().StringVar(&policyEditPlaybook, "playbook", "", "Updated role playbook; use '-' for stdin")
	policyEditCmd.Flags().StringVar(&policyEditTitle, "title", "", "Updated invariant title")
}

func runPolicyEdit(cmd *cobra.Command, args []string) error {
	hasInvariant := policyEditInvariant != ""
	hasRole := policyEditRole != ""
	if hasInvariant == hasRole {
		return fmt.Errorf("exactly one of --invariant or --role is required")
	}

	cfg, err := loadAndValidateConfig()
	if err != nil {
		return err
	}

	if hasInvariant {
		return editInvariant(cfg)
	}
	return editRole(cfg)
}

func editInvariant(cfg *config.Config) error {
	guidance, err := readContentFlag(policyEditGuidance)
	if err != nil {
		return err
	}

	return mutateActivePolicy(cfg, func(bundle *client.PolicyBundle, _ *client.ActivePolicyResponse) error {
		for i, inv := range bundle.Invariants {
			if inv.ID == policyEditInvariant {
				if policyEditTitle != "" {
					bundle.Invariants[i].Title = policyEditTitle
				}
				if strings.TrimSpace(guidance) != "" {
					bundle.Invariants[i].BodyMD = guidance
				}
				fmt.Fprintf(os.Stderr, "Updated invariant %q\n", policyEditInvariant)
				return nil
			}
		}
		return fmt.Errorf("invariant %q not found (available: %s)", policyEditInvariant, availableInvariantIDs(bundle))
	})
}

func editRole(cfg *config.Config) error {
	name := config.NormalizeRole(policyEditRole)
	if name == "" {
		return fmt.Errorf("invalid role name %q", policyEditRole)
	}

	playbook, err := readContentFlag(policyEditPlaybook)
	if err != nil {
		return err
	}

	return mutateActivePolicy(cfg, func(bundle *client.PolicyBundle, _ *client.ActivePolicyResponse) error {
		existing, ok := bundle.Roles[name]
		if !ok {
			return fmt.Errorf("role %q not found (available: %s)", name, availableRoleNames(bundle))
		}
		if strings.TrimSpace(playbook) != "" {
			existing.PlaybookMD = playbook
		}
		bundle.Roles[name] = existing
		fmt.Fprintf(os.Stderr, "Updated role %q\n", name)
		return nil
	})
}

// --- :policy delete ---

var (
	policyDeleteInvariant string
	policyDeleteRole      string
)

var policyDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete an invariant or role from the active policy",
	Long: `Delete an invariant or role playbook from the active project policy.

Examples:
  bdh :policy delete --invariant my-rule
  bdh :policy delete --role reviewer`,
	RunE: runPolicyDelete,
}

func init() {
	policyDeleteCmd.Flags().StringVar(&policyDeleteInvariant, "invariant", "", "Invariant ID to delete")
	policyDeleteCmd.Flags().StringVar(&policyDeleteRole, "role", "", "Role name to delete")
}

func runPolicyDelete(cmd *cobra.Command, args []string) error {
	hasInvariant := policyDeleteInvariant != ""
	hasRole := policyDeleteRole != ""
	if hasInvariant == hasRole {
		return fmt.Errorf("exactly one of --invariant or --role is required")
	}

	cfg, err := loadAndValidateConfig()
	if err != nil {
		return err
	}

	if hasInvariant {
		return deleteInvariant(cfg)
	}
	return deleteRole(cfg)
}

func deleteInvariant(cfg *config.Config) error {
	return mutateActivePolicy(cfg, func(bundle *client.PolicyBundle, _ *client.ActivePolicyResponse) error {
		for i, inv := range bundle.Invariants {
			if inv.ID == policyDeleteInvariant {
				bundle.Invariants = append(bundle.Invariants[:i], bundle.Invariants[i+1:]...)
				fmt.Fprintf(os.Stderr, "Deleted invariant %q\n", policyDeleteInvariant)
				return nil
			}
		}
		return fmt.Errorf("invariant %q not found (available: %s)", policyDeleteInvariant, availableInvariantIDs(bundle))
	})
}

func deleteRole(cfg *config.Config) error {
	name := config.NormalizeRole(policyDeleteRole)
	if name == "" {
		return fmt.Errorf("invalid role name %q", policyDeleteRole)
	}

	return mutateActivePolicy(cfg, func(bundle *client.PolicyBundle, _ *client.ActivePolicyResponse) error {
		if _, ok := bundle.Roles[name]; !ok {
			return fmt.Errorf("role %q not found (available: %s)", name, availableRoleNames(bundle))
		}
		delete(bundle.Roles, name)
		fmt.Fprintf(os.Stderr, "Deleted role %q\n", name)
		return nil
	})
}

// capitalizeWords uppercases the first letter of each word.
func capitalizeWords(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// --- Formatting helpers ---

func availableInvariantIDs(bundle *client.PolicyBundle) string {
	ids := make([]string, len(bundle.Invariants))
	for i, inv := range bundle.Invariants {
		ids[i] = inv.ID
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return "(none)"
	}
	return strings.Join(ids, ", ")
}

func availableRoleNames(bundle *client.PolicyBundle) string {
	names := make([]string, 0, len(bundle.Roles))
	for name := range bundle.Roles {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}
