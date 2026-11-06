package parentactioncmd

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func TestSourceResynchronizationAdmitsOnlyMechanicalControllerOperations(t *testing.T) {
	allowed := []controller.ParentCompletionOperation{
		{Surface: controller.PublicationOperationSurface, Operation: controller.PublicationOperationAdoptMutable},
		{Surface: controller.PublicationOperationSurface, Operation: controller.PublicationOperationRebind},
		{Surface: controller.PublicationOperationSurface, Operation: controller.PublicationOperationAdopt},
		{Surface: controller.PublicationOperationSurface, Operation: controller.PublicationOperationAdoptSource},
	}
	for _, operation := range allowed {
		if !sourceResynchronizationAllowed(operation, false) {
			t.Fatalf("canonical mechanical operation refused: %#v", operation)
		}
	}
	disallowed := []controller.ParentCompletionOperation{
		{Surface: controller.PublicationOperationSurface, Operation: controller.PublicationOperationAccept},
		{Surface: controller.PublicationOperationSurface, Operation: controller.PublicationOperationPromote},
		{Surface: controller.PublicationOperationSurface, Operation: controller.PublicationOperationPublish},
		{Surface: controller.PublicationOperationSurface, Operation: controller.PublicationOperationFinalValidation},
		{Surface: controller.PublicationOperationSurface, Operation: controller.PublicationOperationInstall},
		{Surface: controller.PublicationOperationSurface, Operation: controller.PublicationOperationPostInstallSmoke},
		{Surface: controller.ExecutionOperationSurface, Operation: controller.PublicationOperationParentOnlyMaterialize},
		{Surface: controller.SuccessorStartSurface, Operation: controller.PublicationOperationSuccessorStart},
		{Surface: controller.PublicationOperationSurface, Operation: controller.PublicationOperationRecover},
		{Surface: controller.ExecutionOperationSurface, Operation: controller.PublicationOperationRecover},
		{Surface: "", Operation: controller.PublicationOperationNone},
	}
	for _, operation := range disallowed {
		if sourceResynchronizationAllowed(operation, false) {
			t.Fatalf("non-source operation admitted: %#v", operation)
		}
	}
	for _, operation := range []controller.ParentCompletionOperation{
		{Surface: controller.ExecutionOperationSurface, Operation: "cleanup"},
		{Surface: controller.ExecutionOperationSurface, Operation: "materialize"},
	} {
		if !sourceResynchronizationAllowed(operation, true) {
			t.Fatalf("source-associated execution transition refused: %#v", operation)
		}
		if sourceResynchronizationAllowed(operation, false) {
			t.Fatalf("unrelated task materialization admitted before source adoption: %#v", operation)
		}
	}
	for _, surface := range []string{controller.PublicationOperationSurface, controller.ExecutionOperationSurface} {
		if sourceResynchronizationAllowed(controller.ParentCompletionOperation{Surface: surface, Operation: controller.PublicationOperationRecover}, true) {
			t.Fatalf("in-flight recovery must remain separately authorized: %s", surface)
		}
	}
}
