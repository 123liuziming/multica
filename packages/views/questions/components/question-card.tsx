"use client";

import { useState } from "react";
import { Check, CheckCircle2, Clock, HelpCircle, XCircle } from "lucide-react";
import type { AgentQuestion } from "@multica/core/types";
import { useAnswerQuestion } from "@multica/core/questions";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { RadioGroup, RadioGroupItem } from "@multica/ui/components/ui/radio-group";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { useT } from "../../i18n";

interface QuestionCardProps {
  question: AgentQuestion;
}

const CUSTOM_RADIO_VALUE = "__custom__";

export function QuestionCard({ question }: QuestionCardProps) {
  const { t } = useT("questions");
  const isPending = question.status === "pending";
  const isMulti = question.multi_select;

  // Multi: list of selected indices + "custom" toggle + textarea
  const [multiSelected, setMultiSelected] = useState<number[]>([]);
  const [multiCustomChecked, setMultiCustomChecked] = useState(false);
  const [multiCustomText, setMultiCustomText] = useState("");
  // Single: a string — option index OR CUSTOM_RADIO_VALUE
  const [singleSelected, setSingleSelected] = useState<string>("");
  const [singleCustomText, setSingleCustomText] = useState("");

  const answer = useAnswerQuestion();

  const canSubmitMulti =
    multiSelected.length > 0 ||
    (multiCustomChecked && multiCustomText.trim().length > 0);
  const canSubmitSingle =
    (singleSelected !== "" && singleSelected !== CUSTOM_RADIO_VALUE) ||
    (singleSelected === CUSTOM_RADIO_VALUE && singleCustomText.trim().length > 0);
  const canSubmit = isPending && (isMulti ? canSubmitMulti : canSubmitSingle);

  const handleSubmit = async () => {
    if (!canSubmit) return;
    const payload = isMulti
      ? {
          option_indices: multiSelected,
          custom_text:
            multiCustomChecked && multiCustomText.trim().length > 0
              ? multiCustomText.trim()
              : undefined,
        }
      : singleSelected === CUSTOM_RADIO_VALUE
        ? {
            option_indices: [],
            custom_text: singleCustomText.trim(),
          }
        : {
            option_indices: [parseInt(singleSelected, 10)],
            custom_text: undefined,
          };
    try {
      await answer.mutateAsync({ id: question.id, data: payload });
      toast.success(t(($) => $.submit_button));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
    }
  };

  const tag = isMulti ? t(($) => $.tag_multi) : t(($) => $.tag_single);

  return (
    <div className="rounded-lg border bg-card p-4 shadow-sm">
      <div className="mb-2 flex items-start gap-2">
        <StatusIcon status={question.status} />
        <div className="flex-1 min-w-0">
          <div className="text-sm font-semibold">
            <span
              className={cn(
                "mr-1.5 inline-block rounded px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wider",
                isMulti
                  ? "bg-amber-500/15 text-amber-700 dark:text-amber-400"
                  : "bg-sky-500/15 text-sky-700 dark:text-sky-400",
              )}
            >
              [{tag}]
            </span>
            {question.header}
          </div>
          <div className="mt-0.5 whitespace-pre-wrap text-sm text-foreground">
            {question.question}
          </div>
        </div>
      </div>

      {isPending ? (
        <div className="mt-3 space-y-3">
          {isMulti ? (
            <MultiSelectAsking
              options={question.options}
              selected={multiSelected}
              onChange={setMultiSelected}
              customChecked={multiCustomChecked}
              onCustomToggle={setMultiCustomChecked}
              customText={multiCustomText}
              onCustomTextChange={setMultiCustomText}
              tCustomLabel={t(($) => $.option_custom_label)}
              tCustomDescription={t(($) => $.option_custom_description)}
              tCustomPlaceholder={t(($) => $.custom_answer_placeholder)}
            />
          ) : (
            <SingleSelectAsking
              options={question.options}
              selected={singleSelected}
              onChange={setSingleSelected}
              customText={singleCustomText}
              onCustomTextChange={setSingleCustomText}
              tCustomLabel={t(($) => $.option_custom_label)}
              tCustomDescription={t(($) => $.option_custom_description)}
              tCustomPlaceholder={t(($) => $.custom_answer_placeholder)}
            />
          )}
          <div className="flex items-center justify-end gap-2">
            {!canSubmit && (
              <span className="text-xs text-muted-foreground">
                {t(($) => $.submit_button_disabled_hint)}
              </span>
            )}
            <Button
              type="button"
              onClick={handleSubmit}
              disabled={!canSubmit || answer.isPending}
            >
              {t(($) => $.submit_button)}
            </Button>
          </div>
        </div>
      ) : (
        <>
          <div className="mt-3">
            <ReadonlyOptionList question={question} />
          </div>
          <AnsweredMeta question={question} />
        </>
      )}
    </div>
  );
}

function StatusIcon({ status }: { status: AgentQuestion["status"] }) {
  if (status === "answered") {
    return <CheckCircle2 className="mt-0.5 h-4 w-4 text-emerald-600" />;
  }
  if (status === "cancelled") {
    return <XCircle className="mt-0.5 h-4 w-4 text-muted-foreground" />;
  }
  return <HelpCircle className="mt-0.5 h-4 w-4 text-primary" />;
}

// ─── Asking: multi-select ──────────────────────────────────────────────────
function MultiSelectAsking({
  options,
  selected,
  onChange,
  customChecked,
  onCustomToggle,
  customText,
  onCustomTextChange,
  tCustomLabel,
  tCustomDescription,
  tCustomPlaceholder,
}: {
  options: AgentQuestion["options"];
  selected: number[];
  onChange: (next: number[]) => void;
  customChecked: boolean;
  onCustomToggle: (next: boolean) => void;
  customText: string;
  onCustomTextChange: (s: string) => void;
  tCustomLabel: string;
  tCustomDescription: string;
  tCustomPlaceholder: string;
}) {
  return (
    <div className="space-y-1.5">
      {options.map((opt, i) => {
        const checked = selected.includes(i);
        return (
          <label
            key={i}
            className={cn(
              "flex cursor-pointer items-start gap-2 rounded-md border p-2 transition-colors hover:bg-accent/40",
              checked && "border-primary bg-primary/5",
            )}
          >
            <Checkbox
              checked={checked}
              onCheckedChange={(state) => {
                if (state) onChange([...selected, i].sort());
                else onChange(selected.filter((s) => s !== i));
              }}
            />
            <div className="flex-1 text-sm">
              <div className="font-medium">{opt.label}</div>
              {opt.description && (
                <div className="text-xs text-muted-foreground">{opt.description}</div>
              )}
            </div>
          </label>
        );
      })}
      <label
        className={cn(
          "flex cursor-pointer items-start gap-2 rounded-md border p-2 transition-colors hover:bg-accent/40",
          customChecked && "border-primary bg-primary/5",
        )}
      >
        <Checkbox
          checked={customChecked}
          onCheckedChange={(state) => onCustomToggle(state === true)}
        />
        <div className="flex-1 text-sm">
          <div className="font-medium">{tCustomLabel}</div>
          <div className="text-xs text-muted-foreground">{tCustomDescription}</div>
          {customChecked && (
            <Textarea
              value={customText}
              onChange={(e) => onCustomTextChange(e.target.value)}
              placeholder={tCustomPlaceholder}
              rows={3}
              className="mt-2"
              onClick={(e) => e.stopPropagation()}
            />
          )}
        </div>
      </label>
    </div>
  );
}

// ─── Asking: single-select ─────────────────────────────────────────────────
function SingleSelectAsking({
  options,
  selected,
  onChange,
  customText,
  onCustomTextChange,
  tCustomLabel,
  tCustomDescription,
  tCustomPlaceholder,
}: {
  options: AgentQuestion["options"];
  selected: string;
  onChange: (i: string) => void;
  customText: string;
  onCustomTextChange: (s: string) => void;
  tCustomLabel: string;
  tCustomDescription: string;
  tCustomPlaceholder: string;
}) {
  return (
    <RadioGroup value={selected || undefined} onValueChange={onChange}>
      <div className="space-y-1.5">
        {options.map((opt, i) => {
          const isSelected = selected === String(i);
          return (
            <label
              key={i}
              className={cn(
                "flex cursor-pointer items-start gap-2 rounded-md border p-2 transition-colors hover:bg-accent/40",
                isSelected && "border-primary bg-primary/5",
              )}
            >
              <RadioGroupItem value={String(i)} className="mt-0.5" />
              <div className="flex-1 text-sm">
                <div className="font-medium">{opt.label}</div>
                {opt.description && (
                  <div className="text-xs text-muted-foreground">{opt.description}</div>
                )}
              </div>
            </label>
          );
        })}
        <label
          className={cn(
            "flex cursor-pointer items-start gap-2 rounded-md border p-2 transition-colors hover:bg-accent/40",
            selected === CUSTOM_RADIO_VALUE && "border-primary bg-primary/5",
          )}
        >
          <RadioGroupItem value={CUSTOM_RADIO_VALUE} className="mt-0.5" />
          <div className="flex-1 text-sm">
            <div className="font-medium">{tCustomLabel}</div>
            <div className="text-xs text-muted-foreground">{tCustomDescription}</div>
            {selected === CUSTOM_RADIO_VALUE && (
              <Textarea
                value={customText}
                onChange={(e) => onCustomTextChange(e.target.value)}
                placeholder={tCustomPlaceholder}
                rows={3}
                className="mt-2"
                onClick={(e) => e.stopPropagation()}
              />
            )}
          </div>
        </label>
      </div>
    </RadioGroup>
  );
}

// ─── Readonly view (answered / cancelled) ──────────────────────────────────
function ReadonlyOptionList({ question }: { question: AgentQuestion }) {
  const { t } = useT("questions");
  const selected = new Set(question.answer_option_indices ?? []);
  const customSelected = !!question.answer_custom_text;
  const isMulti = question.multi_select;

  return (
    <div className="space-y-1.5">
      {question.options.map((opt, i) => (
        <ReadonlyRow
          key={i}
          selected={selected.has(i)}
          isMulti={isMulti}
          label={opt.label}
          description={opt.description}
        />
      ))}
      <ReadonlyRow
        selected={customSelected}
        isMulti={isMulti}
        label={t(($) => $.option_custom_label)}
        description={
          customSelected ? question.answer_custom_text ?? undefined : t(($) => $.option_custom_description)
        }
        emphasizeDescription={customSelected}
      />
    </div>
  );
}

function ReadonlyRow({
  selected,
  isMulti,
  label,
  description,
  emphasizeDescription = false,
}: {
  selected: boolean;
  isMulti: boolean;
  label: string;
  description?: string;
  emphasizeDescription?: boolean;
}) {
  return (
    <div
      className={cn(
        "flex cursor-default items-start gap-2 rounded-md border p-2 text-sm",
        selected && "border-primary bg-primary/5",
      )}
    >
      <ReadonlyMarker selected={selected} isMulti={isMulti} />
      <div className="flex-1 min-w-0">
        <div className="font-medium">{label}</div>
        {description && (
          <div
            className={cn(
              "text-xs",
              emphasizeDescription
                ? "whitespace-pre-wrap text-foreground"
                : "text-muted-foreground",
            )}
          >
            {description}
          </div>
        )}
      </div>
    </div>
  );
}

/**
 * Visual mirror of the asking-state controls so the readonly list looks
 * the same shape it would have looked when the user picked it:
 *  - multi → square checkbox, with a check icon when selected
 *  - single → round radio, with an inner dot when selected
 *
 * Uses arbitrary `rounded-[...]` values so JIT picks up exactly the radius
 * we want — `rounded-sm` on a tiny marker is too round to read as a square.
 */
function ReadonlyMarker({
  selected,
  isMulti,
}: {
  selected: boolean;
  isMulti: boolean;
}) {
  const shape = isMulti ? "rounded-[3px]" : "rounded-full";
  return (
    <span
      className={cn(
        "mt-0.5 inline-flex size-4 shrink-0 items-center justify-center border",
        shape,
        selected
          ? "border-primary bg-primary text-primary-foreground"
          : "border-muted-foreground/50 bg-background",
      )}
    >
      {selected && isMulti && <Check className="h-3 w-3" strokeWidth={3} />}
      {selected && !isMulti && (
        <span className="size-1.5 rounded-full bg-primary-foreground" />
      )}
    </span>
  );
}

function AnsweredMeta({ question }: { question: AgentQuestion }) {
  const { t } = useT("questions");
  const { getMemberName } = useActorName();
  if (question.status === "cancelled") {
    return (
      <p className="mt-2 text-xs italic text-muted-foreground">
        {t(($) => $.answer_status_cancelled)}
      </p>
    );
  }
  const answererId = question.answered_by_user_id;
  return (
    <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
      {answererId && (
        <span className="flex items-center gap-1.5">
          <span className="text-muted-foreground/80">
            {t(($) => $.answered_by_label)}
          </span>
          <ActorAvatar actorType="member" actorId={answererId} size={14} />
          <span className="font-medium text-foreground">
            {getMemberName(answererId)}
          </span>
        </span>
      )}
      <span className="flex items-center gap-1">
        <Clock className="h-3 w-3" />
        {t(($) => $.answer_status_answered_at, {
          time: question.answered_at ? new Date(question.answered_at).toLocaleString() : "",
        })}
      </span>
    </div>
  );
}
