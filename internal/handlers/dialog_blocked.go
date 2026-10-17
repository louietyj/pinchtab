package handlers

import (
	"fmt"
	"net/http"

	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/httpx"
	"github.com/pinchtab/pinchtab/internal/remedy"
)

const dialogBlockedCode = "dialog_blocked"

const dialogBlockedHint = "a JavaScript dialog is open on this tab and blocks every page interaction until it is answered. It belongs to this tab, so retrying, activating the tab or opening a fresh one cannot clear it. Use pinchtab dialog dismiss to cancel it instead of accepting, or pass --dialog-action accept|dismiss on the action that opens the dialog."

var dialogBlockedRemedy = remedy.Declare("pinchtab dialog accept")

const dialogBlockedStepRemedy = "answer it with pinchtab dialog accept or pinchtab dialog dismiss, or pass --dialog-action accept on the action that opens the dialog"

func dialogBlockedStepError(message string) string {
	return message + "; " + dialogBlockedStepRemedy
}

func pendingTabDialog(b bridge.BridgeAPI, tabID string) *bridge.DialogState {
	if b == nil || tabID == "" {
		return nil
	}
	dm := b.GetDialogManager()
	if dm == nil {
		return nil
	}
	return dm.GetPending(tabID)
}

func dialogBlockedMessage(tabID string, dialog *bridge.DialogState) string {
	return fmt.Sprintf("tab %s is blocked by a JavaScript dialog (%s: %q)", tabID, dialog.Type, dialog.Message)
}

func dialogBlockedDetails(tabID string, dialog *bridge.DialogState) map[string]any {
	details := remedy.Details(dialogBlockedHint, dialogBlockedRemedy.Remedy())
	details["tabId"] = tabID
	details["dialogType"] = dialog.Type
	details["dialogMessage"] = dialog.Message
	return details
}

func writeDialogBlocked(w http.ResponseWriter, tabID string, dialog *bridge.DialogState, message string) {
	if message == "" {
		message = dialogBlockedMessage(tabID, dialog)
	}
	httpx.ErrorCode(w, http.StatusConflict, dialogBlockedCode, message, false, dialogBlockedDetails(tabID, dialog))
}

func (h *Handlers) refuseIfDialogBlocked(w http.ResponseWriter, tabID string) bool {
	dialog := pendingTabDialog(h.Bridge, tabID)
	if dialog == nil {
		return false
	}
	writeDialogBlocked(w, tabID, dialog, "")
	return true
}

func dialogBlockedActionResult(index int, tabID string, dialog *bridge.DialogState) actionResult {
	return actionResult{
		Index:   index,
		Success: false,
		Error:   dialogBlockedStepError(dialogBlockedMessage(tabID, dialog)),
		Code:    dialogBlockedCode,
		Details: dialogBlockedDetails(tabID, dialog),
	}
}
