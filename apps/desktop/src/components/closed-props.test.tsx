// This file passes forbidden props on purpose, to prove the types reject them.
/* oxlint-disable shadcn/no-restyle, react/forbid-component-props */
import { render, screen } from "@testing-library/react";
import { AgentxMark } from "@/components/agentx-mark";
import { CommandField } from "@/components/command-field";
import { Icon } from "@/components/icon";
import { KeyHint } from "@/components/key-hint";
import { Toaster } from "@/components/toast";
import { Button } from "@/components/ui/button";
import { PlaceholderScreen } from "@/screens/placeholder";

// Our components take no `className` or `style`: every visual choice is a typed prop.
// `pnpm typecheck` fails if any line marked @ts-expect-error starts to compile.
// The function is never called; it exists for the type checker.
export function rejectedProps() {
  return (
    <>
      {/* @ts-expect-error className is not a Button prop */}
      <Button className="mt-2" />
      {/* @ts-expect-error style is not a Button prop */}
      <Button style={{ "--gap": "0" }} />
      {/* @ts-expect-error variant is a closed set */}
      <Button variant="link" />
      {/* @ts-expect-error className is not an Icon prop */}
      <Icon name="search" className="text-text-primary" />
      {/* @ts-expect-error style is not an Icon prop */}
      <Icon name="search" style={{ "--tone": "0" }} />
      {/* @ts-expect-error tone is a closed set */}
      <Icon name="search" tone="red" />
      {/* @ts-expect-error size is a closed set */}
      <Icon name="search" size={17} />
      {/* @ts-expect-error className is not an AgentxMark prop */}
      <AgentxMark size={26} className="rounded-card" />
      {/* @ts-expect-error size is a closed set */}
      <AgentxMark size={32} />
      {/* @ts-expect-error className is not a CommandField prop */}
      <CommandField onOpen={() => {}} className="w-full" />
      {/* @ts-expect-error style is not a KeyHint prop */}
      <KeyHint style={{ "--gap": "0" }}>esc</KeyHint>
      {/* @ts-expect-error className is not a Toaster prop */}
      <Toaster className="bottom-0" />
      {/* @ts-expect-error className is not a PlaceholderScreen prop */}
      <PlaceholderScreen title="Inbox" className="p-0" />
    </>
  );
}

describe("closed props", () => {
  it("keeps Button's classes even when a className is forced past the types", () => {
    const forced = { className: "mt-2" } as object;
    render(
      <Button variant="primary" {...forced}>
        Save
      </Button>,
    );
    expect(screen.getByRole("button", { name: "Save" })).not.toHaveClass("mt-2");
  });

  it("gives each product mark size its own radius", () => {
    render(
      <>
        <AgentxMark size={26} />
        <AgentxMark size={40} />
      </>,
    );
    const [small, large] = screen.getAllByRole("img", { name: "agentx" });
    expect(small).toHaveClass("rounded-shell-logo");
    expect(large).toHaveClass("rounded-startup-logo");
  });
});
