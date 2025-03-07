package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/twpayne/chezmoi/v2/internal/chezmoi"
)

type boolModifier int

const (
	boolModifierSet            boolModifier = 1
	boolModifierLeaveUnchanged boolModifier = 0
	boolModifierClear          boolModifier = -1
)

type conditionModifier int

const (
	conditionModifierLeaveUnchanged conditionModifier = iota
	conditionModifierClearOnce
	conditionModifierSetOnce
	conditionModifierClearOnChange
	conditionModifierSetOnChange
)

type orderModifier int

const (
	orderModifierSetBefore      orderModifier = -2
	orderModifierClearBefore    orderModifier = -1
	orderModifierLeaveUnchanged orderModifier = 0
	orderModifierClearAfter     orderModifier = 1
	orderModifierSetAfter       orderModifier = 2
)

type attrModifier struct {
	condition  conditionModifier
	empty      boolModifier
	encrypted  boolModifier
	exact      boolModifier
	executable boolModifier
	order      orderModifier
	private    boolModifier
	readOnly   boolModifier
	template   boolModifier
}

func (c *Config) newChattrCmd() *cobra.Command {
	attrs := []string{
		"after", "a",
		"before", "b",
		"empty", "e",
		"encrypted",
		"exact",
		"executable", "x",
		"once", "o",
		"onchange",
		"private", "p",
		"readonly", "r",
		"template", "t",
	}
	validArgs := make([]string, 0, 4*len(attrs))
	for _, attribute := range attrs {
		validArgs = append(validArgs, attribute, "-"+attribute, "+"+attribute, "no"+attribute)
	}

	chattrCmd := &cobra.Command{
		Use:       "chattr attributes target...",
		Short:     "Change the attributes of a target in the source state",
		Long:      mustLongHelp("chattr"),
		Example:   example("chattr"),
		Args:      cobra.MinimumNArgs(2),
		ValidArgs: validArgs,
		RunE:      c.makeRunEWithSourceState(c.runChattrCmd),
		Annotations: map[string]string{
			modifiesSourceDirectory: "true",
		},
	}

	return chattrCmd
}

func (c *Config) runChattrCmd(cmd *cobra.Command, args []string, sourceState *chezmoi.SourceState) error {
	// LATER should the core functionality of chattr move to chezmoi.SourceState?

	am, err := parseAttrModifier(args[0])
	if err != nil {
		return err
	}

	targetRelPaths, err := c.targetRelPaths(sourceState, args[1:], targetRelPathsOptions{
		mustBeInSourceState: true,
	})
	if err != nil {
		return err
	}

	// Sort targets in reverse so we update children before their parent
	// directories.
	sort.Slice(targetRelPaths, func(i, j int) bool {
		return targetRelPaths[i] > targetRelPaths[j]
	})

	encryptedSuffix := sourceState.Encryption().EncryptedSuffix()
	for _, targetRelPath := range targetRelPaths {
		sourceStateEntry := sourceState.MustEntry(targetRelPath)
		sourceRelPath := sourceStateEntry.SourceRelPath()
		parentSourceRelPath, fileSourceRelPath := sourceRelPath.Split()
		parentRelPath := parentSourceRelPath.RelPath()
		fileRelPath := fileSourceRelPath.RelPath()
		switch sourceStateEntry := sourceStateEntry.(type) {
		case *chezmoi.SourceStateDir:
			if newBaseNameRelPath := chezmoi.RelPath(am.modifyDirAttr(sourceStateEntry.Attr).SourceName()); newBaseNameRelPath != fileRelPath {
				oldSourceAbsPath := c.SourceDirAbsPath.Join(parentRelPath, fileRelPath)
				newSourceAbsPath := c.SourceDirAbsPath.Join(parentRelPath, newBaseNameRelPath)
				if err := c.sourceSystem.Rename(oldSourceAbsPath, newSourceAbsPath); err != nil {
					return err
				}
			}
		case *chezmoi.SourceStateFile:
			// FIXME encrypted attribute changes
			// FIXME when changing encrypted attribute add new file before removing old one
			if newBaseNameRelPath := chezmoi.RelPath(am.modifyFileAttr(sourceStateEntry.Attr).SourceName(encryptedSuffix)); newBaseNameRelPath != fileRelPath {
				oldSourceAbsPath := c.SourceDirAbsPath.Join(parentRelPath, fileRelPath)
				newSourceAbsPath := c.SourceDirAbsPath.Join(parentRelPath, newBaseNameRelPath)
				if err := c.sourceSystem.Rename(oldSourceAbsPath, newSourceAbsPath); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// modify returns the modified value of b.
func (m boolModifier) modify(b bool) bool {
	switch m {
	case boolModifierSet:
		return true
	case boolModifierLeaveUnchanged:
		return b
	case boolModifierClear:
		return false
	default:
		panic(fmt.Sprintf("%d: unknown bool modifier", m))
	}
}

// modify returns the modified value of condition.
func (m conditionModifier) modify(condition chezmoi.ScriptCondition) chezmoi.ScriptCondition {
	switch m {
	case conditionModifierLeaveUnchanged:
		return condition
	case conditionModifierClearOnce:
		if condition == chezmoi.ScriptConditionOnce {
			return chezmoi.ScriptConditionAlways
		}
		return condition
	case conditionModifierSetOnce:
		return chezmoi.ScriptConditionOnce
	case conditionModifierClearOnChange:
		if condition == chezmoi.ScriptConditionOnChange {
			return chezmoi.ScriptConditionAlways
		}
		return condition
	case conditionModifierSetOnChange:
		return chezmoi.ScriptConditionOnChange
	default:
		panic(fmt.Sprintf("%d: unknown order modifier", m))
	}
}

// modify returns the modified value of order.
func (m orderModifier) modify(order chezmoi.ScriptOrder) chezmoi.ScriptOrder {
	switch m {
	case orderModifierSetBefore:
		return chezmoi.ScriptOrderBefore
	case orderModifierClearBefore:
		if order == chezmoi.ScriptOrderBefore {
			return chezmoi.ScriptOrderDuring
		}
		return order
	case orderModifierLeaveUnchanged:
		return order
	case orderModifierClearAfter:
		if order == chezmoi.ScriptOrderAfter {
			return chezmoi.ScriptOrderDuring
		}
		return order
	case orderModifierSetAfter:
		return chezmoi.ScriptOrderAfter
	default:
		panic(fmt.Sprintf("%d: unknown order modifier", m))
	}
}

// parseAttrModifier parses the attrMmodifier from s.
func parseAttrModifier(s string) (*attrModifier, error) {
	am := &attrModifier{}
	for _, modifierStr := range strings.Split(s, ",") {
		modifierStr = strings.TrimSpace(modifierStr)
		if modifierStr == "" {
			continue
		}
		var bm boolModifier
		var attribute string
		switch {
		case modifierStr[0] == '-':
			bm = boolModifierClear
			attribute = modifierStr[1:]
		case modifierStr[0] == '+':
			bm = boolModifierSet
			attribute = modifierStr[1:]
		case strings.HasPrefix(modifierStr, "no"):
			bm = boolModifierClear
			attribute = modifierStr[2:]
		default:
			bm = boolModifierSet
			attribute = modifierStr
		}
		switch attribute {
		case "after", "a":
			switch bm {
			case boolModifierClear:
				am.order = orderModifierClearAfter
			case boolModifierLeaveUnchanged:
				am.order = orderModifierLeaveUnchanged
			case boolModifierSet:
				am.order = orderModifierSetAfter
			}
		case "before", "b":
			switch bm {
			case boolModifierClear:
				am.order = orderModifierClearBefore
			case boolModifierLeaveUnchanged:
				am.order = orderModifierLeaveUnchanged
			case boolModifierSet:
				am.order = orderModifierSetBefore
			}
		case "empty", "e":
			am.empty = bm
		case "encrypted":
			am.encrypted = bm
		case "exact":
			am.exact = bm
		case "executable", "x":
			am.executable = bm
		case "once", "o":
			switch bm {
			case boolModifierClear:
				am.condition = conditionModifierClearOnce
			case boolModifierSet:
				am.condition = conditionModifierSetOnce
			}
		case "onchange":
			switch bm {
			case boolModifierClear:
				am.condition = conditionModifierClearOnChange
			case boolModifierSet:
				am.condition = conditionModifierSetOnChange
			}
		case "private", "p":
			am.private = bm
		case "readonly", "r":
			am.readOnly = bm
		case "template", "t":
			am.template = bm
		default:
			return nil, fmt.Errorf("%s: unknown attribute", attribute)
		}
	}
	return am, nil
}

// modifyDirAttr returns the modified value of dirAttr.
func (am *attrModifier) modifyDirAttr(dirAttr chezmoi.DirAttr) chezmoi.DirAttr {
	return chezmoi.DirAttr{
		TargetName: dirAttr.TargetName,
		Exact:      am.exact.modify(dirAttr.Exact),
		Private:    am.private.modify(dirAttr.Private),
		ReadOnly:   am.readOnly.modify(dirAttr.ReadOnly),
	}
}

// modifyFileAttr returns the modified value of fileAttr.
func (am *attrModifier) modifyFileAttr(fileAttr chezmoi.FileAttr) chezmoi.FileAttr {
	switch fileAttr.Type {
	case chezmoi.SourceFileTypeFile:
		return chezmoi.FileAttr{
			TargetName: fileAttr.TargetName,
			Type:       chezmoi.SourceFileTypeFile,
			Empty:      am.empty.modify(fileAttr.Empty),
			Encrypted:  am.encrypted.modify(fileAttr.Encrypted),
			Executable: am.executable.modify(fileAttr.Executable),
			Private:    am.private.modify(fileAttr.Private),
			ReadOnly:   am.readOnly.modify(fileAttr.ReadOnly),
			Template:   am.template.modify(fileAttr.Template),
		}
	case chezmoi.SourceFileTypeModify:
		return chezmoi.FileAttr{
			TargetName: fileAttr.TargetName,
			Type:       chezmoi.SourceFileTypeModify,
			Executable: am.executable.modify(fileAttr.Executable),
			Private:    am.private.modify(fileAttr.Private),
			ReadOnly:   am.readOnly.modify(fileAttr.ReadOnly),
			Template:   am.template.modify(fileAttr.Template),
		}
	case chezmoi.SourceFileTypeCreate:
		return chezmoi.FileAttr{
			TargetName: fileAttr.TargetName,
			Type:       chezmoi.SourceFileTypeCreate,
			Encrypted:  am.encrypted.modify(fileAttr.Encrypted),
			Executable: am.executable.modify(fileAttr.Executable),
			Private:    am.private.modify(fileAttr.Private),
			ReadOnly:   am.readOnly.modify(fileAttr.ReadOnly),
			Template:   am.template.modify(fileAttr.Template),
		}
	case chezmoi.SourceFileTypeScript:
		return chezmoi.FileAttr{
			TargetName: fileAttr.TargetName,
			Type:       chezmoi.SourceFileTypeScript,
			Condition:  am.condition.modify(fileAttr.Condition),
			Order:      am.order.modify(fileAttr.Order),
		}
	case chezmoi.SourceFileTypeSymlink:
		return chezmoi.FileAttr{
			TargetName: fileAttr.TargetName,
			Type:       chezmoi.SourceFileTypeSymlink,
			Template:   am.template.modify(fileAttr.Template),
		}
	default:
		panic(fmt.Sprintf("%d: unknown source file type", fileAttr.Type))
	}
}
