package daemon

import (
	"strings"
	"testing"
)

func TestFormatAnswersFramesUserAnswerWithoutProductName(t *testing.T) {
	got := formatAnswers([]createdQuestion{
		{
			Question: "Which path should I take?",
			Options: []QuestionOption{
				{Label: "Fast"},
				{Label: "Safe", Description: "Use the conservative path"},
			},
			Status:              "answered",
			AnswerOptionIndices: []int{1},
			AnswerCustomText:    "Please keep the scope narrow.",
		},
	})

	for _, want := range []string{
		"The user answered the question. Continue using the answer below.",
		"Question: Which path should I take?",
		`Answer: "Safe" (Use the conservative path)`,
		"Additional answer: Please keep the scope narrow.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted answer missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(strings.ToLower(got), "multica") {
		t.Fatalf("formatted answer should not mention product name:\n%s", got)
	}
}

func TestFormatAnswersFramesMultipleUserAnswers(t *testing.T) {
	got := formatAnswers([]createdQuestion{
		{
			Question:            "First?",
			Options:             []QuestionOption{{Label: "A"}},
			Status:              "answered",
			AnswerOptionIndices: []int{0},
		},
		{
			Question: "Second?",
			Status:   "cancelled",
		},
	})

	for _, want := range []string{
		"The user answered the questions. Continue using the answers below.",
		`Q1. Question: First?`,
		`Answer: "A"`,
		`Q2. Question: Second?`,
		`Status: cancelled by the user`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted answer missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(strings.ToLower(got), "multica") {
		t.Fatalf("formatted answer should not mention product name:\n%s", got)
	}
}

func TestFormatAnswersUsesCustomTextAsAnswerWhenNoOptionSelected(t *testing.T) {
	got := formatAnswers([]createdQuestion{
		{
			Question:         "What should I do?",
			Status:           "answered",
			AnswerCustomText: "Use the smallest safe change.",
		},
	})

	for _, want := range []string{
		"Question: What should I do?",
		"Answer: Use the smallest safe change.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted answer missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "Additional answer:") {
		t.Fatalf("custom-only answer should not be framed as additional:\n%s", got)
	}
}
