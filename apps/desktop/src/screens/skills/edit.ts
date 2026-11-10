// Editing a skill in the editor the user chose (Settings, Editor): which skills offer Edit, and
// the file that opens. The app opens the file and nothing else; the inventory that follows an edit
// shows what changed, as it does for an edit made anywhere else.
import type { SkillRow } from "#/screens/skills/projection.ts";

/**
 * Whether the detail pane offers Edit: for a skill the user may write to without losing the edit
 * or diverging silently. That is one of your own skills, and a skill of a source that is already
 * edited (`state` `modified`), whose edits an update keeps and the "Edited locally" banner shows.
 * Not an unmodified skill of a source, whose first edit would quietly turn it into one the update
 * merges; not a plugin's skill, which the plugin overwrites; and not an unmanaged skill or one a
 * client holds on its own, which agentx does not track.
 */
export const canEdit = (row: SkillRow) =>
  row.entry !== undefined &&
  (row.kind === "yours" || (row.kind === "source" && row.modified));

/** The `SKILL.md` of the library skill `name`, `library` being where agentx keeps the library. */
export const skillFile = (library: string, name: string) =>
  `${library}/${name}/SKILL.md`;
