// The Settings page (DESIGN.md §6 "Settings"): every machine setting on one page. Each row reads
// a store slice and writes only through the CLI command named beside it; the app writes no file
// of the CLI's. The one setting the app keeps itself is Editor, an app preference (ADR 0013).
//
// The page is the title and the list of sections below. A section is a component that renders
// `SettingsSection` (`section.tsx`) with its label, the line under it and its content, or nothing
// when it has nothing to show; the page draws the hairlines between them. To add a section, write
// it in its own file in this folder and add it to `sections` at its slot below, in this order:
// This machine, Account remote, Sources, Comparing edits, Editor, Storage, Command line, Export
// settings, Import settings. Insert the new entry under its slot's comment and keep the comment.
import type { ComponentType } from "react";

import { AccountRemoteSection } from "#/screens/settings/account-remote.tsx";
import { CommandLineSection } from "#/screens/settings/command-line.tsx";
import { ComparingEditsSection } from "#/screens/settings/comparing-edits.tsx";
import { EditorSection } from "#/screens/settings/editor.tsx";
import { ExportSettingsSection } from "#/screens/settings/export-settings.tsx";
import { ImportSettingsSection } from "#/screens/settings/import-settings.tsx";
import { SourcesSection } from "#/screens/settings/sources.tsx";
import { StorageSection } from "#/screens/settings/storage.tsx";
import { ThisMachineSection } from "#/screens/settings/this-machine.tsx";

interface Section {
  /** Names the section, as React's key. */
  id: string;
  Section: ComponentType;
}

/** The sections, top to bottom. */
const sections: Section[] = [
  { Section: ThisMachineSection, id: "this-machine" },
  // Slot: Account remote.
  { Section: AccountRemoteSection, id: "account-remote" },
  // Slot: Sources.
  { Section: SourcesSection, id: "sources" },
  { Section: ComparingEditsSection, id: "comparing-edits" },
  { Section: EditorSection, id: "editor" },
  { Section: StorageSection, id: "storage" },
  { Section: CommandLineSection, id: "command-line" },
  // Slot: Export settings, Import settings.
  { Section: ExportSettingsSection, id: "export-settings" },
  { Section: ImportSettingsSection, id: "import-settings" },
];

export const SettingsScreen = () => (
  <div className="pt-page-top px-page pb-page gap-page-title flex h-full flex-col overflow-auto">
    <h1 className="type-page-title text-text-primary m-0">Settings</h1>
    <div className="flex flex-col">
      {sections.map(({ id, Section }) => (
        <Section key={id} />
      ))}
    </div>
  </div>
);
