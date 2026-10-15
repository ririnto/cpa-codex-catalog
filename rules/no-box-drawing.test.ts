import assert from "node:assert/strict";
import { test } from "node:test";

import { lint } from "markdownlint/sync";

import boxDrawing from "./no-box-drawing.ts";

const violations = (input: string) => {
  const result = lint({
    config: { default: false, "docs/no-box-drawing": true },
    customRules: [boxDrawing],
    strings: { input }
  });
  return result.input ?? [];
};

test("accepts characters outside the box-drawing range", () => {
  const input = `root\n+-- child\n|   +-- leaf\n${String.fromCodePoint(0x24_ff)} ${String.fromCodePoint(0x25_80)}\n`;
  assert.deepEqual(violations(input), []);
});

test("reports both boundary characters in the box-drawing range", () => {
  const found = violations("a─\n╿\n");
  assert.deepEqual(
    found.map(({ errorRange, lineNumber }) => ({ errorRange, lineNumber })),
    [
      { errorRange: [2, 1], lineNumber: 1 },
      { errorRange: [1, 1], lineNumber: 2 }
    ]
  );
  assert.deepEqual(
    found.map(({ ruleNames }) => ruleNames),
    [["docs/no-box-drawing"], ["docs/no-box-drawing"]]
  );
});

test("rejects box-drawing characters inside fenced code", () => {
  const [first] = violations("```text\n│ inner ── node\n```\n");
  assert.deepEqual(
    { errorRange: first?.errorRange, lineNumber: first?.lineNumber },
    { errorRange: [1, 1], lineNumber: 2 }
  );
});
