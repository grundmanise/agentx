// Editor: the application a skill opens in when the user edits it (Create and edit, and Edit in
// the Skills detail pane). One choice: System default, each editor found on this machine, or a
// custom command line, to which the file's path is added. It is the app's own preference, which
// `src/lib/editor.ts` reads and saves; no CLI command runs, and no setting of the CLI changes.
import { useEffect, useId, useState } from "react";

import { Input } from "#/components/ui/input.tsx";
import { RadioGroup, RadioGroupItem } from "#/components/ui/radio-group.tsx";
import { detectEditors, readEditor, saveEditor } from "#/lib/editor.ts";
import type { DetectedEditor, EditorChoice } from "#/lib/editor.ts";
import { SettingsSection } from "#/screens/settings/section.tsx";

/** The placeholder of the Custom command field. */
export const COMMAND_PLACEHOLDER = "code --wait";

/** A choice's value in the radio group. */
const valueOf = (editor: EditorChoice) => {
  switch (editor.kind) {
    case "app": {
      return `app:${editor.id}`;
    }
    case "custom": {
      return "custom";
    }
    case "system": {
      return "system";
    }
    default: {
      return "system";
    }
  }
};

/**
 * The editors to list: those found, then the one chosen when it is no longer found, so the choice
 * still shows.
 */
const listed = (
  found: DetectedEditor[],
  editor: EditorChoice
): DetectedEditor[] =>
  editor.kind === "app" && !found.some((e) => e.id === editor.id)
    ? [...found, { id: editor.id, name: editor.name }]
    : found;

const EditorChoices = ({
  initial,
  found,
}: {
  initial: EditorChoice;
  found: DetectedEditor[];
}) => {
  const [editor, setEditor] = useState(initial);
  // What the Custom command field holds, kept while another choice is made.
  const [command, setCommand] = useState(
    initial.kind === "custom" ? initial.command : ""
  );
  const hintId = useId();
  const editors = listed(found, editor);

  const choose = (next: EditorChoice) => {
    setEditor(next);
    void saveEditor(next);
  };

  const saveCommand = () => {
    if (editor.kind === "custom" && editor.command !== command.trim()) {
      choose({ command: command.trim(), kind: "custom" });
    }
  };

  return (
    <div className="flex flex-col gap-3">
      <RadioGroup
        aria-label="Editor"
        value={valueOf(editor)}
        onValueChange={(value) => {
          if (value === "system") {
            choose({ kind: "system" });
            return;
          }
          if (value === "custom") {
            choose({ command: command.trim(), kind: "custom" });
            return;
          }
          const app = editors.find((e) => `app:${e.id}` === value);
          if (app !== undefined) {
            choose({ id: app.id, kind: "app", name: app.name });
          }
        }}
      >
        <RadioGroupItem
          value="system"
          label="System default"
          checked={editor.kind === "system"}
        />
        {editors.map((e) => (
          <RadioGroupItem
            key={e.id}
            value={`app:${e.id}`}
            label={e.name}
            checked={editor.kind === "app" && editor.id === e.id}
          />
        ))}
        <RadioGroupItem
          value="custom"
          label="Custom command"
          checked={editor.kind === "custom"}
        />
      </RadioGroup>
      {editor.kind === "custom" && (
        <div className="gap-input-label flex flex-col">
          <Input
            kind="mono"
            aria-label="Custom command"
            aria-describedby={hintId}
            placeholder={COMMAND_PLACEHOLDER}
            value={command}
            onChange={(e) => {
              setCommand(e.target.value);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                saveCommand();
              }
            }}
            onBlur={saveCommand}
          />
          <p id={hintId} className="type-secondary text-text-hint m-0">
            The file path is added at the end.
          </p>
        </div>
      )}
    </div>
  );
};

export const EditorSection = () => {
  const [read, setRead] = useState<
    { editor: EditorChoice; found: DetectedEditor[] } | undefined
  >();
  useEffect(() => {
    let current = true;
    void (async () => {
      const [editor, found] = await Promise.all([
        readEditor(),
        detectEditors(),
      ]);
      if (current) {
        setRead({ editor, found });
      }
    })();
    return () => {
      current = false;
    };
  }, []);
  return (
    <SettingsSection
      title="Editor"
      description="The application that opens a skill when you edit it."
    >
      {read !== undefined && (
        <EditorChoices initial={read.editor} found={read.found} />
      )}
    </SettingsSection>
  );
};
