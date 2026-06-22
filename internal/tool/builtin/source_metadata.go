package builtin

import (
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/resultv2"
)

func hostBuiltinSource(origin, integrity string) tool.ToolSourceMetadata {
	if integrity == "" {
		integrity = resultv2.IntegrityHostVerified
	}
	return tool.ToolSourceMetadata{
		Kind:        tool.ToolSourceBuiltin,
		ProducerID:  "emoagent.builtin",
		RuntimeKind: resultv2.RuntimeHost,
		DefaultLabels: resultv2.ContentLabels{
			Executor:             resultv2.ExecutorHostBuiltin,
			Origin:               origin,
			Integrity:            integrity,
			InstructionAuthority: resultv2.InstructionDataOnly,
		},
	}
}

func workspaceFileSource() tool.ToolSourceMetadata {
	return hostBuiltinSource(resultv2.OriginWorkspaceFile, resultv2.IntegrityHashVerified)
}

func externalWebSource() tool.ToolSourceMetadata {
	return hostBuiltinSource(resultv2.OriginExternalWeb, resultv2.IntegrityUnverified)
}

func hostFileSource() tool.ToolSourceMetadata {
	return hostBuiltinSource(resultv2.OriginHostFile, resultv2.IntegrityHashVerified)
}

func systemGeneratedSource() tool.ToolSourceMetadata {
	return hostBuiltinSource(resultv2.OriginSystemGenerated, resultv2.IntegrityHostVerified)
}

func managedHostProcessSource() tool.ToolSourceMetadata {
	source := hostBuiltinSource(resultv2.OriginSystemGenerated, resultv2.IntegrityHostVerified)
	source.RuntimeKind = resultv2.RuntimeManagedHostProcess
	source.DefaultLabels.Executor = resultv2.ExecutorManagedHost
	return source
}

func unsafeHostSource() tool.ToolSourceMetadata {
	source := hostBuiltinSource(resultv2.OriginSystemGenerated, resultv2.IntegrityUnverified)
	source.RuntimeKind = resultv2.RuntimeHost
	return source
}
