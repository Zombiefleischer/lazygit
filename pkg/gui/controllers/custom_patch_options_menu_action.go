package controllers

import (
	"fmt"

	"github.com/jesseduffield/lazygit/pkg/commands/types/enums"
	"github.com/jesseduffield/lazygit/pkg/gui/types"
)

type CustomPatchOptionsMenuAction struct {
	c *ControllerCommon
}

func (self *CustomPatchOptionsMenuAction) Call() error {
	if !self.c.Git().Patch.PatchBuilder.Active() {
		return self.c.ErrorMsg(self.c.Tr.NoPatchError)
	}

	menuItems := []*types.MenuItem{
		{
			Label:   "reset patch",
			OnPress: self.c.Helpers().PatchBuilding.Reset,
			Key:     'c',
		},
		{
			Label:   "apply patch",
			OnPress: func() error { return self.handleApplyPatch(false) },
			Key:     'a',
		},
		{
			Label:   "apply patch in reverse",
			OnPress: func() error { return self.handleApplyPatch(true) },
			Key:     'r',
		},
	}

	if self.c.Git().Patch.PatchBuilder.CanRebase && self.c.Git().Status.WorkingTreeState() == enums.REBASE_MODE_NONE {
		menuItems = append(menuItems, []*types.MenuItem{
			{
				Label:   fmt.Sprintf("remove patch from original commit (%s)", self.c.Git().Patch.PatchBuilder.To),
				OnPress: self.handleDeletePatchFromCommit,
				Key:     'd',
			},
			{
				Label:   "move patch out into index",
				OnPress: self.handleMovePatchIntoWorkingTree,
				Key:     'i',
			},
			{
				Label:   "move patch into new commit",
				OnPress: self.handlePullPatchIntoNewCommit,
				Key:     'n',
			},
		}...)

		if self.c.CurrentContext().GetKey() == self.c.Contexts().LocalCommits.GetKey() {
			selectedCommit := self.c.Contexts().LocalCommits.GetSelected()
			if selectedCommit != nil && self.c.Git().Patch.PatchBuilder.To != selectedCommit.Sha {
				// adding this option to index 1
				menuItems = append(
					menuItems[:1],
					append(
						[]*types.MenuItem{
							{
								Label:   fmt.Sprintf("move patch to selected commit (%s)", selectedCommit.Sha),
								OnPress: self.handleMovePatchToSelectedCommit,
								Key:     'm',
							},
						}, menuItems[1:]...,
					)...,
				)
			}
		}
	}

	menuItems = append(menuItems, []*types.MenuItem{
		{
			Label:   "copy patch to clipboard",
			OnPress: func() error { return self.copyPatchToClipboard() },
			Key:     'y',
		},
	}...)

	return self.c.Menu(types.CreateMenuOptions{Title: self.c.Tr.PatchOptionsTitle, Items: menuItems})
}

func (self *CustomPatchOptionsMenuAction) getPatchCommitIndex() int {
	for index, commit := range self.c.Model().Commits {
		if commit.Sha == self.c.Git().Patch.PatchBuilder.To {
			return index
		}
	}
	return -1
}

func (self *CustomPatchOptionsMenuAction) validateNormalWorkingTreeState() (bool, error) {
	if self.c.Git().Status.WorkingTreeState() != enums.REBASE_MODE_NONE {
		return false, self.c.ErrorMsg(self.c.Tr.CantPatchWhileRebasingError)
	}
	return true, nil
}

func (self *CustomPatchOptionsMenuAction) returnFocusFromPatchExplorerIfNecessary() error {
	if self.c.CurrentContext().GetKey() == self.c.Contexts().CustomPatchBuilder.GetKey() {
		return self.c.Helpers().PatchBuilding.Escape()
	}
	return nil
}

func (self *CustomPatchOptionsMenuAction) handleDeletePatchFromCommit() error {
	if ok, err := self.validateNormalWorkingTreeState(); !ok {
		return err
	}

	if err := self.returnFocusFromPatchExplorerIfNecessary(); err != nil {
		return err
	}

	return self.c.WithWaitingStatus(self.c.Tr.RebasingStatus, func() error {
		commitIndex := self.getPatchCommitIndex()
		self.c.LogAction(self.c.Tr.Actions.RemovePatchFromCommit)
		err := self.c.Git().Patch.DeletePatchesFromCommit(self.c.Model().Commits, commitIndex)
		return self.c.Helpers().MergeAndRebase.CheckMergeOrRebase(err)
	})
}

func (self *CustomPatchOptionsMenuAction) handleMovePatchToSelectedCommit() error {
	if ok, err := self.validateNormalWorkingTreeState(); !ok {
		return err
	}

	if err := self.returnFocusFromPatchExplorerIfNecessary(); err != nil {
		return err
	}

	return self.c.WithWaitingStatus(self.c.Tr.RebasingStatus, func() error {
		commitIndex := self.getPatchCommitIndex()
		self.c.LogAction(self.c.Tr.Actions.MovePatchToSelectedCommit)
		err := self.c.Git().Patch.MovePatchToSelectedCommit(self.c.Model().Commits, commitIndex, self.c.Contexts().LocalCommits.GetSelectedLineIdx())
		return self.c.Helpers().MergeAndRebase.CheckMergeOrRebase(err)
	})
}

func (self *CustomPatchOptionsMenuAction) handleMovePatchIntoWorkingTree() error {
	if ok, err := self.validateNormalWorkingTreeState(); !ok {
		return err
	}

	if err := self.returnFocusFromPatchExplorerIfNecessary(); err != nil {
		return err
	}

	pull := func(stash bool) error {
		return self.c.WithWaitingStatus(self.c.Tr.RebasingStatus, func() error {
			commitIndex := self.getPatchCommitIndex()
			self.c.LogAction(self.c.Tr.Actions.MovePatchIntoIndex)
			err := self.c.Git().Patch.MovePatchIntoIndex(self.c.Model().Commits, commitIndex, stash)
			return self.c.Helpers().MergeAndRebase.CheckMergeOrRebase(err)
		})
	}

	if self.c.Helpers().WorkingTree.IsWorkingTreeDirty() {
		return self.c.Confirm(types.ConfirmOpts{
			Title:  self.c.Tr.MustStashTitle,
			Prompt: self.c.Tr.MustStashWarning,
			HandleConfirm: func() error {
				return pull(true)
			},
		})
	} else {
		return pull(false)
	}
}

func (self *CustomPatchOptionsMenuAction) handlePullPatchIntoNewCommit() error {
	if ok, err := self.validateNormalWorkingTreeState(); !ok {
		return err
	}

	if err := self.returnFocusFromPatchExplorerIfNecessary(); err != nil {
		return err
	}

	return self.c.WithWaitingStatus(self.c.Tr.RebasingStatus, func() error {
		commitIndex := self.getPatchCommitIndex()
		self.c.LogAction(self.c.Tr.Actions.MovePatchIntoNewCommit)
		err := self.c.Git().Patch.PullPatchIntoNewCommit(self.c.Model().Commits, commitIndex)
		return self.c.Helpers().MergeAndRebase.CheckMergeOrRebase(err)
	})
}

func (self *CustomPatchOptionsMenuAction) handleApplyPatch(reverse bool) error {
	if err := self.returnFocusFromPatchExplorerIfNecessary(); err != nil {
		return err
	}

	action := self.c.Tr.Actions.ApplyPatch
	if reverse {
		action = "Apply patch in reverse"
	}
	self.c.LogAction(action)
	if err := self.c.Git().Patch.ApplyCustomPatch(reverse); err != nil {
		return self.c.Error(err)
	}
	return self.c.Refresh(types.RefreshOptions{Mode: types.ASYNC})
}

func (self *CustomPatchOptionsMenuAction) copyPatchToClipboard() error {
	patch := self.c.Git().Patch.PatchBuilder.RenderAggregatedPatch(true)

	self.c.LogAction(self.c.Tr.Actions.CopyPatchToClipboard)
	if err := self.c.OS().CopyToClipboard(patch); err != nil {
		return self.c.Error(err)
	}

	self.c.Toast(self.c.Tr.PatchCopiedToClipboard)

	return nil
}
