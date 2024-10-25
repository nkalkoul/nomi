package commit

import (
 "context"
 "encoding/json"
 "fmt"
 "strings"
 "time"

 "github.com/nullswan/nomi/internal/chat"
 "github.com/nullswan/nomi/internal/tools"
)

// TODO:nullswan): Handle stash reference correctly to avoid any TOCTOU issues.
// TODO(nullswan): Add memory on the commit plan, preference, commonly used prefix, scopes, modules, and components.
// TODO(nullswan): Handle progressive commit plan, reducing the delay, ask to merge using file name when too many lines are changed.

const agentPrompt = "Create a commit plan in JSON format for staging patches and creating commits using Git, adhering to provided guidelines.\n\n## JSON Structure\n\nThe commit plan should be represented as a JSON object containing a list of actions. Each action includes both a `patch` and a `commit`:\n\n- **Action**: Contains the file path, the Git patch, and the commit message.\n\n### Steps\n\n1. **Analyze the Git Diff:**\n - Group related changes into features or fixes.\n - Determine necessity for multiple commits for unrelated changes.\n\n2. **Prepare Staging Commands:**\n - Use `git apply --cached` to stage specific lines.\n - Ensure accurate staging for atomic, feature-specific commits.\n\n3. **Generate Commit Messages:**\n - Maintain present tense with an appropriate prefix and scope.\n - Exclude meaningless component names (e.g., \"internal\") from commit titles.\n - Preserve only significant component names.\n - Keep messages concise, within 75 characters for titles.\n\n## Commit Message Specifications\n\n- **Tense:** Present\n- **Prefixes:** `feat:`, `fix:`, `docs:`, `style:`, `refactor:`, `perf:`, `test:`, `chore:`, `ci:`\n- **Scope:** Specify affected significant component/module in parentheses.\n- **No Body:** Keep it concise unless additional description is necessary.\n\n### Additional Guidelines\n\n- Group related changes for the same feature in a single commit.\n- Use multiple commits for unrelated changes.\n- Ensure messages are clear, concise, and specific.\n- Maintain consistent scoping based on file paths and modules.\n\n## Output Format\n\nProvide the commit plan in a plain JSON format containing the necessary actions with both `patch` and `commit` details.\n\n## Example Commit Plan\n\n{\n \"commitPlan\": [\n {\n \"filePath\": \"cmd/cli/main.go\",\n \"patch\": \"diff --git a/cmd/cli/main.go b/cmd/cli/main.go\\nindex 83c3e7f..b4b49b6 100644\\n--- a/cmd/cli/main.go\\n+++ b/cmd/cli/main.go\\n@@ -10,6 +10,7 @@ package main\\n import (\\n \\\"fmt\\\"\\n \\\"os\\\"\\n+ \\\"time\\\"\\n )\\n\",\n \"commitMessage\": \"feat(cmd/cli): add time import\"\n },\n {\n \"filePath\": \"internal/prompts/templates.go\",\n \"patch\": \"diff --git a/internal/prompts/templates.go b/internal/prompts/templates.go\\nindex e69de29..f8a7e5d 100644\\n--- a/internal/prompts/templates.go\\n+++ b/internal/prompts/templates.go\\n@@ -0,0 +1 @@\\n+// New templates for prompts\\n\",\n \"commitMessage\": \"docs(prompts): add new templates for prompts\"\n }\n ]\n}"

type commitPlan struct {
 CommitPlan []action `json:"commitPlan"`
}

type action struct {
 FilePath string `json:"filePath"`
 Patch string `json:"patch"`
 CommitMessage string `json:"commitMessage"`
}

func OnStart(
 ctx context.Context,
 console tools.Console,
 selector tools.Selector,
 logger tools.Logger,
 textToJSON tools.TextToJSONBackend,
 inputArea tools.InputArea,
 conversation chat.Conversation,
) error {
 logger.Info("Starting commit usecase")

 conversation.AddMessage(
 chat.NewMessage(chat.Role(chat.RoleSystem), agentPrompt),
 )

 if err := checkGitRepository(ctx, console); err != nil {
 return fmt.Errorf("not a git repository: %w", err)
 }

 logger.Info("Stashing changes")
 err := stashChanges(ctx, console)
 if err != nil {
 return fmt.Errorf("failed to stash changes: %w", err)
 }

 // Unstash changes directly so you can continue working on the changes
 err = unstashChanges(ctx, console)
 if err != nil {
 return fmt.Errorf("failed to unstash changes: %w", err)
 }

 defer func() {
 err = deleteStash(ctx, console)
 if err != nil {
 logger.Error("Failed to delete stash: " + err.Error())
 }
 }()

 logger.Info("Getting stash diff")
 buffer, err := getStashDiff(ctx, console)
 if err != nil {
 return fmt.Errorf("failed to get stash diff: %w", err)
 }

 logger.Debug("Stash diff: " + buffer)
 if buffer == "" {
 logger.Info("No changes to commit")
 return nil
 }

 conversation.AddMessage(
 chat.NewMessage(chat.Role(chat.RoleUser), buffer),
 )

 for {
 select {
 case <-ctx.Done():
 return fmt.Errorf("context cancelled")
 default:
 logger.Info("Creating commit plan")
 resp, err := textToJSON.Do(ctx, conversation)
 if err != nil {
 return fmt.Errorf("failed to convert text to JSON: %w", err)
 }
 logger.Debug("Raw Commit plan: " + resp)

 var plan commitPlan
 if err := json.Unmarshal([]byte(resp), &plan); err != nil {
 return fmt.Errorf("failed to unmarshal commit plan: %w", err)
 }

 logger.Println("Commit Plan:")
 for _, a := range plan.CommitPlan {
 logger.Println("\t" + a.CommitMessage)
 }

 if !selector.SelectBool(
 "Do you want to commit these changes?",
 true,
 ) {
 newInstructions := inputArea.Read(">>> ")
 conversation.AddMessage(
 chat.NewMessage(
 chat.Role(chat.RoleUser),
 newInstructions,
 ),
 )

 continue
 }

 var errors []error
 for i, a := range plan.CommitPlan {
 // Patch should end with a newline
 if !strings.HasSuffix(a.Patch, "\n") {
 a.Patch += "\n"
 }

 cmd := tools.NewCommand(
 "git",
 "apply",
 "--cached",
 "-p1",
 "-",
 ).WithInput(a.Patch)

 result, err := console.Exec(ctx, cmd)
 if err != nil {
 errors = append(
 errors,
 fmt.Errorf("failed to apply patch %d: %w", i, err),
 )
 continue
 }
 if !result.Success() {
 errors = append(errors, fmt.Errorf(
 "failed to apply patch %d: %s",
 i,
 result.Error,
 ))
 continue
 }

 cmd = tools.NewCommand(
 "git",
 "commit",
 "--message",
 a.CommitMessage,
 )
 result, err = console.Exec(ctx, cmd)
 if err != nil {
 errors = append(
 errors,
 fmt.Errorf("failed to commit changes %d: %w", i, err),
 )
 continue
 }
 if !result.Success() {
 errors = append(errors, fmt.Errorf(
 "failed to commit changes %d: %s",
 i,
 result.Error,
 ))
 continue
 }

 logger.Info("Committed " + a.CommitMessage)
 }

 return nil
 }
 }
}

func checkGitRepository(ctx context.Context, console tools.Console) error {
 cmd := tools.NewCommand("git", "rev-parse", "--is-inside-work-tree")
 result, err := console.Exec(ctx, cmd)
 if err != nil || !result.Success() {
 return fmt.Errorf("not a git repository")
 }
 return nil
}

func stashChanges(ctx context.Context, console tools.Console) error {
 timestamp := time.Now().Format("20060102T150405")
 stashName := fmt.Sprintf("nomi-stash-%s", timestamp)
 cmd := tools.NewCommand(
 "git",
 "stash",
 "push",
 "--include-untracked",
 "--message",
 stashName,
 )
 result, err := console.Exec(ctx, cmd)
 if err != nil {
 return fmt.Errorf("failed to stash changes: %w", err)
 }
 if !result.Success() {
 if result.Error != "" {
 return fmt.Errorf("failed to stash changes: %s", result.Error)
 }
 if result.Output != "" {
 return fmt.Errorf("failed to stash changes: %s", result.Output)
 }
 return fmt.Errorf("failed to stash changes and received no output")
 }

 // Extract stash reference from the output
 stashRef := ""
 lines := strings.Split(result.Output, "\n")
 if len(lines) > 0 {
 parts := strings.Split(lines[0], ":")
 if len(parts) > 0 {
 stashRef = strings.TrimSpace(parts[0])
 }
 }
 if stashRef == "" {
 return fmt.Errorf("unable to retrieve stash reference")
 }

 return nil
}

func getStashDiff(
 ctx context.Context,
 console tools.Console,
) (string, error) {
 cmd := tools.NewCommand(
 "git",
 "stash",
 "show",
 "--include-untracked",
 "--patch",
 "stash@{0}",
 )
 result, err := console.Exec(ctx, cmd)
 if err != nil || !result.Success() {
 return "", fmt.Errorf("failed to show stash diff")
 }
 return result.Output, nil
}

func unstashChanges(
 ctx context.Context,
 console tools.Console,
) error {
 cmd := tools.NewCommand("git", "stash", "apply", "stash@{0}")
 result, err := console.Exec(ctx, cmd)
 if err != nil || !result.Success() {
 return fmt.Errorf("failed to unstash changes")
 }

 cmd = tools.NewCommand("git", "reset")
 result, err = console.Exec(ctx, cmd)
 if err != nil || !result.Success() {
 return fmt.Errorf("failed to reset changes")
 }

 return nil
}

func deleteStash(
 ctx context.Context,
 console tools.Console,
) error {
 cmd := tools.NewCommand("git", "stash", "drop", "stash@{0}")
 result, err := console.Exec(ctx, cmd)
 if err != nil || !result.Success() {
 return fmt.Errorf("failed to delete stash")
 }
 return nil
}
