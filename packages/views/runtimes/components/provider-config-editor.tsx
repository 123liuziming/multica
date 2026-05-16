"use client";

import { useState } from "react";
import { Settings2 } from "lucide-react";
import { toast } from "sonner";
import TOML from "smol-toml";
import type { AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useUpdateRuntime } from "@multica/core/runtimes/mutations";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";

const CLAUDE_PLACEHOLDER = JSON.stringify(
  {
    env: {
      ANTHROPIC_API_KEY: "<your-api-key>",
      ANTHROPIC_AUTH_TOKEN: "<your-auth-token>",
      ANTHROPIC_BASE_URL: "<your-base-url>",
      ANTHROPIC_MODEL: "<model-name>",
      ANTHROPIC_SMALL_FAST_MODEL: "<small-model-name>",
    },
    permissions: {
      dangerouslySkipPermissions: true,
    },
    skipDangerousModePermissionPrompt: true,
  },
  null,
  2,
);

const CODEX_PLACEHOLDER = `approvals_reviewer = "user"

[notice]
hide_full_access_warning = true

[sandbox_workspace_write]
network_access = true

[projects."<project-path>"]
trust_level = "trusted"

[mcp_servers."<server-name>"]
url = "<mcp-server-url>"
bearer_token_env_var = "<ENV_VAR_NAME>"`;

const PLACEHOLDER_BY_PROVIDER: Record<string, string> = {
  claude: CLAUDE_PLACEHOLDER,
  codex: CODEX_PLACEHOLDER,
};

function isCodex(provider: string) {
  return provider === "codex";
}

function jsonToToml(obj: Record<string, unknown>): string {
  return TOML.stringify(obj);
}

function tomlToJson(text: string): Record<string, unknown> {
  return TOML.parse(text) as Record<string, unknown>;
}

export function ProviderConfigEditor({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const updateRuntime = useUpdateRuntime(wsId);
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | null>(null);

  const codex = isCodex(runtime.provider);

  const hasConfig =
    runtime.provider_config != null &&
    Object.keys(runtime.provider_config).length > 0;

  const handleOpen = () => {
    if (runtime.provider_config) {
      if (codex) {
        try {
          setValue(jsonToToml(runtime.provider_config));
        } catch {
          setValue(JSON.stringify(runtime.provider_config, null, 2));
        }
      } else {
        setValue(JSON.stringify(runtime.provider_config, null, 2));
      }
    } else {
      setValue("");
    }
    setError(null);
    setOpen(true);
  };

  const handleSave = () => {
    const trimmed = value.trim();
    let parsed: Record<string, unknown> | null = null;
    if (trimmed) {
      try {
        parsed = codex ? tomlToJson(trimmed) : JSON.parse(trimmed);
      } catch {
        setError(
          codex
            ? t(($) => $.detail.provider_config_invalid_toml)
            : t(($) => $.detail.provider_config_invalid_json),
        );
        return;
      }
    }
    updateRuntime.mutate(
      { runtimeId: runtime.id, patch: { provider_config: parsed } },
      {
        onSuccess: () => {
          toast.success(
            parsed
              ? t(($) => $.detail.provider_config_toast_updated)
              : t(($) => $.detail.provider_config_toast_cleared),
          );
          setOpen(false);
        },
        onError: () => {
          toast.error(t(($) => $.detail.provider_config_toast_failed));
        },
      },
    );
  };

  const hint =
    runtime.provider === "claude"
      ? t(($) => $.detail.provider_config_hint_claude)
      : runtime.provider === "codex"
        ? t(($) => $.detail.provider_config_hint_codex)
        : t(($) => $.detail.provider_config_hint_default);

  return (
    <>
      <Button variant="outline" size="sm" className="h-8 gap-1.5" onClick={handleOpen}>
        <Settings2 className="h-3.5 w-3.5" />
        {hasConfig
          ? t(($) => $.detail.provider_config_button)
          : t(($) => $.detail.provider_config_button_empty)}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.detail.provider_config_dialog_title, {
                provider: runtime.provider,
              })}
            </DialogTitle>
            <DialogDescription>{hint}</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <textarea
              className="h-64 w-full rounded-md border bg-muted/30 p-3 font-mono text-xs leading-relaxed focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              value={value}
              onChange={(e) => {
                setValue(e.target.value);
                setError(null);
              }}
              placeholder={
                PLACEHOLDER_BY_PROVIDER[runtime.provider] ?? "{}"
              }
              spellCheck={false}
            />
            {error && (
              <p className="text-xs text-destructive">{error}</p>
            )}
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleSave} disabled={updateRuntime.isPending}>
              {updateRuntime.isPending ? "Saving..." : "Save"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
