import assert from "node:assert/strict";
import { test } from "node:test";

import { lint } from "markdownlint/sync";

import sentencePerLine from "./sentence-per-line.ts";

const violations = (input: string, name = "input") => {
  const result = lint({
    config: { default: false, "docs/sentence-per-line": true },
    customRules: [sentencePerLine],
    strings: { [name]: input }
  });
  return result[name] ?? [];
};

test("accepts one sentence on each source line", () => {
  assert.deepEqual(violations("First sentence.\nSecond sentence.\n"), []);
});

test("reports joined sentences with their source range", () => {
  const found = violations("First sentence. Second sentence.\n");
  const [first] = found;
  assert.equal(found.length, 1);
  assert.deepEqual(
    {
      lineNumber: first?.lineNumber,
      range: first?.errorRange,
      ruleNames: first?.ruleNames
    },
    {
      lineNumber: 1,
      range: [15, 1],
      ruleNames: ["docs/sentence-per-line"]
    }
  );
});

test("reports semicolon-joined sentences with their source range", () => {
  const found = violations("First sentence; Second sentence.\n");
  const [first] = found;
  assert.equal(found.length, 1);
  assert.deepEqual(
    {
      lineNumber: first?.lineNumber,
      range: first?.errorRange,
      ruleNames: first?.ruleNames
    },
    {
      lineNumber: 1,
      range: [15, 1],
      ruleNames: ["docs/sentence-per-line"]
    }
  );
});

test("reports wrapped prose and ignores code and link text", () => {
  const wrapped = violations("First sentence\ncontinues here.\n");
  assert.equal(wrapped.length, 1);
  assert.deepEqual(wrapped[0]?.ruleNames, ["docs/sentence-per-line"]);
  assert.equal(wrapped[0]?.lineNumber, 2);
  assert.deepEqual(wrapped[0]?.errorRange, [1, 1]);
  assert.deepEqual(
    violations(
      "```text\nFirst. Second.\n```\n\n[First. Second](https://example.test) and `Third. Fourth` stay intact.\n"
    ),
    []
  );
});

test("skips license and generated documents", () => {
  assert.deepEqual(violations("First. Second.\n", "LICENSE.md"), []);
  assert.deepEqual(violations("<!-- Generated file -->\nFirst. Second.\n"), []);
});
