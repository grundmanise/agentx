// The name a click is sent under (docs/help-center/telemetry.mdx). PostHog's click autocapture
// masks every element's text and attributes, so a button, menu item or link would show only as
// "clicked button"; the SDK keeps a `data-ph-capture-attribute-<key>` attribute as the event's
// `<key>` property, so each one carries its name in `data-ph-capture-attribute-action`. The shared
// components require it as `track`. A name is fixed text, such as "skills.expand-all", never a
// skill, server, path or anything a user typed.

/** The props of a component a click on which is sent under a name. */
export interface Tracked {
  /** The fixed name a click is sent under, such as "skills.expand-all". */
  track: string;
}

/** The attribute that carries `track` into a click event, as its `action` property. */
export const trackAttribute = (track: string) => ({
  "data-ph-capture-attribute-action": track,
});
