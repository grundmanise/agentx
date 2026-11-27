// Editor: the application a skill opens in when the user edits it (Create and edit, and Edit in
// the Skills detail pane). One choice: System default, each editor found on this machine, or a
// custom command line, to which the file's path is added. Then the Open editor after creating a
// skill switch: off, the Created toast offers Open editor instead. Both are the app's own
// preferences, which come with the app's settings (`src/lib/app-settings.ts`), as the editors
// found do, and which `src/lib/editor.ts` saves; no CLI command runs, and no setting of the CLI
// changes.
import { useId, useState } from "react";

import { Input } from "#/components/ui/input.tsx";
import { RadioGroup, RadioGroupItem } from "#/components/ui/radio-group.tsx";
import { Switch } from "#/components/ui/switch.tsx";
import { useAppSettings } from "#/lib/app-settings.ts";
import { saveEditor, saveOpenAfterCreate } from "#/lib/editor.ts";
import type { DetectedEditor, EditorChoice } from "#/lib/editor.ts";
import { SettingsCard, SettingsRow } from "#/screens/settings/section.tsx";

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
  editor,
  found,
}: {
  editor: EditorChoice;
  found: DetectedEditor[];
}) => {
  // What the Custom command field holds, kept while another choice is made.
  const [command, setCommand] = useState(
    editor.kind === "custom" ? editor.command : ""
  );
  const hintId = useId();
  const editors = listed(found, editor);

  const saveCommand = () => {
    if (editor.kind === "custom" && editor.command !== command.trim()) {
      void saveEditor({ command: command.trim(), kind: "custom" });
    }
  };

  return (
    <div className="flex flex-col gap-3">
      <RadioGroup
        aria-label="Editor"
        value={valueOf(editor)}
        onValueChange={(value) => {
          if (value === "system") {
            void saveEditor({ kind: "system" });
            return;
          }
          if (value === "custom") {
            void saveEditor({ command: command.trim(), kind: "custom" });
            return;
          }
          const app = editors.find((e) => `app:${e.id}` === value);
          if (app !== undefined) {
            void saveEditor({ id: app.id, kind: "app", name: app.name });
          }
        }}
      >
        <RadioGroupItem
          value="system"
          label="System default"
          track="settings.editor-system"
          checked={editor.kind === "system"}
        />
        {editors.map((e) => (
          <RadioGroupItem
            key={e.id}
            value={`app:${e.id}`}
            label={e.name}
            track="settings.editor-app"
            checked={editor.kind === "app" && editor.id === e.id}
          />
        ))}
        <RadioGroupItem
          value="custom"
          label="Custom command"
          track="settings.editor-custom"
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

const OpenAfterCreate = ({ checked }: { checked: boolean }) => {
  const id = useId();
  return (
    <SettingsRow
      name="Open editor after creating a skill"
      htmlFor={id}
      control={
        <Switch
          id={id}
          track="settings.open-after-create-toggle"
          aria-label="Open editor after creating a skill"
          checked={checked}
          onCheckedChange={(on) => {
            void saveOpenAfterCreate(on);
          }}
        />
      }
    />
  );
};

export const EditorCard = () => {
  const settings = useAppSettings((s) => s);
  return (
    <SettingsCard title="Editor">
      {settings !== undefined && (
        <>
          <SettingsRow>
            <EditorChoices
              editor={settings.preferences.editor}
              found={settings.editors}
            />
          </SettingsRow>
          <OpenAfterCreate checked={settings.preferences.open_after_create} />
        </>
      )}
    </SettingsCard>
  );
};
