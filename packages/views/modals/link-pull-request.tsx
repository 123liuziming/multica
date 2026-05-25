"use client";

import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";
import { isImeComposing } from "@multica/core/utils";
import { useLinkPullRequest } from "@multica/core/github/mutations";
import { useT } from "../i18n";

export function LinkPullRequestModal({
  issueId,
  open,
  onClose,
}: {
  issueId: string;
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const link = useLinkPullRequest(issueId);
  const [url, setUrl] = useState("");
  const [title, setTitle] = useState("");
  const [error, setError] = useState<string | null>(null);

  const reset = () => {
    setUrl("");
    setTitle("");
    setError(null);
  };

  const handleClose = () => {
    if (link.isPending) return;
    reset();
    onClose();
  };

  const handleSubmit = () => {
    const trimmedUrl = url.trim();
    if (!trimmedUrl) {
      setError(t(($) => $.detail.link_pull_request_error_url_required));
      return;
    }
    // Server enforces http(s) too, but a same-thread rejection saves a
    // round-trip and protects users from copy-pasting a `javascript:` URL.
    if (!/^https?:\/\//i.test(trimmedUrl)) {
      setError(t(($) => $.detail.link_pull_request_error_url_scheme));
      return;
    }
    setError(null);
    link.mutate(
      { url: trimmedUrl, title: title.trim() || undefined },
      {
        onSuccess: () => {
          toast.success(t(($) => $.detail.link_pull_request_toast_success));
          reset();
          onClose();
        },
        onError: () => {
          toast.error(t(($) => $.detail.link_pull_request_toast_error));
        },
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) handleClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.detail.link_pull_request_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.detail.link_pull_request_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 min-w-0">
          <div>
            <Label htmlFor="link-pr-url" className="text-xs text-muted-foreground">
              {t(($) => $.detail.link_pull_request_url_label)}
            </Label>
            <Input
              id="link-pr-url"
              autoFocus
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder={t(($) => $.detail.link_pull_request_url_placeholder)}
              className="mt-1"
              onKeyDown={(e) => {
                if (isImeComposing(e)) return;
                if (e.key === "Enter") handleSubmit();
              }}
            />
            {error ? (
              <p className="text-[11px] text-rose-600 dark:text-rose-400 mt-1">{error}</p>
            ) : null}
          </div>

          <div>
            <Label htmlFor="link-pr-title" className="text-xs text-muted-foreground">
              {t(($) => $.detail.link_pull_request_title_label)}
            </Label>
            <Input
              id="link-pr-title"
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t(($) => $.detail.link_pull_request_title_placeholder)}
              className="mt-1"
              onKeyDown={(e) => {
                if (isImeComposing(e)) return;
                if (e.key === "Enter") handleSubmit();
              }}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={link.isPending}>
            {t(($) => $.detail.link_pull_request_cancel)}
          </Button>
          <Button onClick={handleSubmit} disabled={!url.trim() || link.isPending}>
            {link.isPending
              ? t(($) => $.detail.link_pull_request_submitting)
              : t(($) => $.detail.link_pull_request_submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
