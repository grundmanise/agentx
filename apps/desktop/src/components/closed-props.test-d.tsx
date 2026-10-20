// This file passes forbidden props on purpose, to prove the types reject them.
/* oxlint-disable shadcn/no-restyle, shadcn/no-inline-styles, react/forbid-component-props */
import { AgentxMark } from "#/components/agentx-mark.tsx";
import { CommandField } from "#/components/command-field.tsx";
import { SearchIcon } from "#/components/icons/search.tsx";
import { SkillsIcon } from "#/components/icons/skills.tsx";
import { KeyHint } from "#/components/key-hint.tsx";
import { Toaster } from "#/components/toast.tsx";
import { Button } from "#/components/ui/button.tsx";
import { PlaceholderScreen } from "#/screens/placeholder.tsx";

// Our components take no `className` or `style`: every visual choice is a typed prop.
// `pnpm typecheck` fails if any line marked @ts-expect-error starts to compile.
// Only the type checker reads this file: the app never imports it and no test runs it.
export const rejectedProps = () => (
  <>
    {/* @ts-expect-error className is not a Button prop */}
    <Button className="mt-2" />
    {/* @ts-expect-error style is not a Button prop */}
    <Button style={{ marginTop: 8 }} />
    {/* @ts-expect-error variant is a closed set */}
    <Button variant="link" />
    {/* @ts-expect-error className is not an icon prop */}
    <SearchIcon className="text-text-primary" />
    {/* @ts-expect-error style is not an icon prop */}
    <SearchIcon style={{ marginTop: 8 }} />
    {/* @ts-expect-error an icon takes its parent's text colour, so it has no tone */}
    <SearchIcon tone="palette" />
    {/* @ts-expect-error an icon takes its parent's text colour, so it has no color */}
    <SkillsIcon color="red" />
    {/* @ts-expect-error each icon holds its own stroke width, so it has no strokeWidth */}
    <SkillsIcon strokeWidth={2} />
    {/* @ts-expect-error an icon size is one of the names in iconSizes */}
    <SearchIcon size="large" />
    {/* @ts-expect-error an icon size is a name, not a pixel value */}
    <SearchIcon size={16} />
    {/* @ts-expect-error className is not an AgentxMark prop */}
    <AgentxMark size={26} className="rounded-card" />
    {/* @ts-expect-error size is a closed set */}
    <AgentxMark size={32} />
    {/* @ts-expect-error className is not a CommandField prop */}
    <CommandField onOpen={() => {}} className="w-full" />
    {/* @ts-expect-error style is not a KeyHint prop */}
    <KeyHint style={{ marginTop: 8 }}>esc</KeyHint>
    {/* @ts-expect-error className is not a Toaster prop */}
    <Toaster className="bottom-0" />
    {/* @ts-expect-error className is not a PlaceholderScreen prop */}
    <PlaceholderScreen title="Inbox" className="p-0" />
  </>
);
