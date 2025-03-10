package controllers

import (
	"strings"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazygit/pkg/commands/git_commands"
	"github.com/jesseduffield/lazygit/pkg/commands/patch"
	"github.com/jesseduffield/lazygit/pkg/gui/types"
)

type StagingController struct {
	baseController
	c *ControllerCommon

	context      types.IPatchExplorerContext
	otherContext types.IPatchExplorerContext

	// if true, we're dealing with the secondary context i.e. dealing with staged file changes
	staged bool
}

var _ types.IController = &StagingController{}

func NewStagingController(
	common *ControllerCommon,
	context types.IPatchExplorerContext,
	otherContext types.IPatchExplorerContext,
	staged bool,
) *StagingController {
	return &StagingController{
		baseController: baseController{},
		c:              common,
		context:        context,
		otherContext:   otherContext,
		staged:         staged,
	}
}

func (self *StagingController) GetKeybindings(opts types.KeybindingsOpts) []*types.Binding {
	return []*types.Binding{
		{
			Key:         opts.GetKey(opts.Config.Universal.OpenFile),
			Handler:     self.OpenFile,
			Description: self.c.Tr.LcOpenFile,
		},
		{
			Key:         opts.GetKey(opts.Config.Universal.Edit),
			Handler:     self.EditFile,
			Description: self.c.Tr.LcEditFile,
		},
		{
			Key:         opts.GetKey(opts.Config.Universal.Return),
			Handler:     self.Escape,
			Description: self.c.Tr.ReturnToFilesPanel,
		},
		{
			Key:         opts.GetKey(opts.Config.Universal.TogglePanel),
			Handler:     self.TogglePanel,
			Description: self.c.Tr.ToggleStagingPanel,
		},
		{
			Key:         opts.GetKey(opts.Config.Universal.Select),
			Handler:     self.ToggleStaged,
			Description: self.c.Tr.StageSelection,
		},
		{
			Key:         opts.GetKey(opts.Config.Universal.Remove),
			Handler:     self.ResetSelection,
			Description: self.c.Tr.ResetSelection,
		},
		{
			Key:         opts.GetKey(opts.Config.Main.EditSelectHunk),
			Handler:     self.EditHunkAndRefresh,
			Description: self.c.Tr.EditHunk,
		},
		{
			Key:         opts.GetKey(opts.Config.Files.CommitChanges),
			Handler:     self.c.Helpers().WorkingTree.HandleCommitPress,
			Description: self.c.Tr.CommitChanges,
		},
		{
			Key:         opts.GetKey(opts.Config.Files.CommitChangesWithoutHook),
			Handler:     self.c.Helpers().WorkingTree.HandleWIPCommitPress,
			Description: self.c.Tr.LcCommitChangesWithoutHook,
		},
		{
			Key:         opts.GetKey(opts.Config.Files.CommitChangesWithEditor),
			Handler:     self.c.Helpers().WorkingTree.HandleCommitEditorPress,
			Description: self.c.Tr.CommitChangesWithEditor,
		},
	}
}

func (self *StagingController) Context() types.Context {
	return self.context
}

func (self *StagingController) GetMouseKeybindings(opts types.KeybindingsOpts) []*gocui.ViewMouseBinding {
	return []*gocui.ViewMouseBinding{}
}

func (self *StagingController) GetOnFocus() func(types.OnFocusOpts) error {
	return func(opts types.OnFocusOpts) error {
		self.c.Views().Staging.Wrap = false
		self.c.Views().StagingSecondary.Wrap = false

		return self.c.Helpers().Staging.RefreshStagingPanel(opts)
	}
}

func (self *StagingController) GetOnFocusLost() func(types.OnFocusLostOpts) error {
	return func(opts types.OnFocusLostOpts) error {
		self.context.SetState(nil)

		if opts.NewContextKey != self.otherContext.GetKey() {
			self.c.Views().Staging.Wrap = true
			self.c.Views().StagingSecondary.Wrap = true
			_ = self.c.Contexts().Staging.Render(false)
			_ = self.c.Contexts().StagingSecondary.Render(false)
		}
		return nil
	}
}

func (self *StagingController) OpenFile() error {
	self.context.GetMutex().Lock()
	defer self.context.GetMutex().Unlock()

	path := self.FilePath()

	if path == "" {
		return nil
	}

	return self.c.Helpers().Files.OpenFile(path)
}

func (self *StagingController) EditFile() error {
	self.context.GetMutex().Lock()
	defer self.context.GetMutex().Unlock()

	path := self.FilePath()

	if path == "" {
		return nil
	}

	lineNumber := self.context.GetState().CurrentLineNumber()
	return self.c.Helpers().Files.EditFileAtLine(path, lineNumber)
}

func (self *StagingController) Escape() error {
	return self.c.PopContext()
}

func (self *StagingController) TogglePanel() error {
	if self.otherContext.GetState() != nil {
		return self.c.PushContext(self.otherContext)
	}

	return nil
}

func (self *StagingController) ToggleStaged() error {
	return self.applySelectionAndRefresh(self.staged)
}

func (self *StagingController) ResetSelection() error {
	reset := func() error { return self.applySelectionAndRefresh(true) }

	if !self.staged && !self.c.UserConfig.Gui.SkipUnstageLineWarning {
		return self.c.Confirm(types.ConfirmOpts{
			Title:         self.c.Tr.UnstageLinesTitle,
			Prompt:        self.c.Tr.UnstageLinesPrompt,
			HandleConfirm: reset,
		})
	}

	return reset()
}

func (self *StagingController) applySelectionAndRefresh(reverse bool) error {
	if err := self.applySelection(reverse); err != nil {
		return err
	}

	return self.c.Refresh(types.RefreshOptions{Scope: []types.RefreshableView{types.FILES, types.STAGING}})
}

func (self *StagingController) applySelection(reverse bool) error {
	self.context.GetMutex().Lock()
	defer self.context.GetMutex().Unlock()

	state := self.context.GetState()
	path := self.FilePath()
	if path == "" {
		return nil
	}

	firstLineIdx, lastLineIdx := state.SelectedRange()
	patchToApply := patch.
		Parse(state.GetDiff()).
		Transform(patch.TransformOpts{
			Reverse:             reverse,
			IncludedLineIndices: patch.ExpandRange(firstLineIdx, lastLineIdx),
			FileNameOverride:    path,
		}).
		FormatPlain()

	if patchToApply == "" {
		return nil
	}

	// apply the patch then refresh this panel
	// create a new temp file with the patch, then call git apply with that patch
	self.c.LogAction(self.c.Tr.Actions.ApplyPatch)
	err := self.c.Git().Patch.ApplyPatch(
		patchToApply,
		git_commands.ApplyPatchOpts{
			Reverse: reverse,
			Cached:  !reverse || self.staged,
		},
	)
	if err != nil {
		return self.c.Error(err)
	}

	if state.SelectingRange() {
		firstLine, _ := state.SelectedRange()
		state.SelectLine(firstLine)
	}

	return nil
}

func (self *StagingController) EditHunkAndRefresh() error {
	if err := self.editHunk(); err != nil {
		return err
	}

	return self.c.Refresh(types.RefreshOptions{Scope: []types.RefreshableView{types.FILES, types.STAGING}})
}

func (self *StagingController) editHunk() error {
	self.context.GetMutex().Lock()
	defer self.context.GetMutex().Unlock()

	state := self.context.GetState()
	path := self.FilePath()
	if path == "" {
		return nil
	}

	hunkStartIdx, hunkEndIdx := state.CurrentHunkBounds()
	patchText := patch.
		Parse(state.GetDiff()).
		Transform(patch.TransformOpts{
			Reverse:             self.staged,
			IncludedLineIndices: patch.ExpandRange(hunkStartIdx, hunkEndIdx),
			FileNameOverride:    path,
		}).
		FormatPlain()

	patchFilepath, err := self.c.Git().Patch.SaveTemporaryPatch(patchText)
	if err != nil {
		return err
	}

	lineOffset := 3
	lineIdxInHunk := state.GetSelectedLineIdx() - hunkStartIdx
	if err := self.c.Helpers().Files.EditFileAtLineAndWait(patchFilepath, lineIdxInHunk+lineOffset); err != nil {
		return err
	}

	editedPatchText, err := self.c.Git().File.Cat(patchFilepath)
	if err != nil {
		return err
	}

	self.c.LogAction(self.c.Tr.Actions.ApplyPatch)

	lineCount := strings.Count(editedPatchText, "\n") + 1
	newPatchText := patch.
		Parse(editedPatchText).
		Transform(patch.TransformOpts{
			IncludedLineIndices: patch.ExpandRange(0, lineCount),
			FileNameOverride:    path,
		}).
		FormatPlain()

	if err := self.c.Git().Patch.ApplyPatch(
		newPatchText,
		git_commands.ApplyPatchOpts{
			Reverse: self.staged,
			Cached:  true,
		},
	); err != nil {
		return self.c.Error(err)
	}

	return nil
}

func (self *StagingController) FilePath() string {
	return self.c.Contexts().Files.GetSelectedPath()
}
