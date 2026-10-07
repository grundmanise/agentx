// Code the design-system lint must reject (`bad(<rule>)`, by that rule) or accept (`ok`), one case
// per line.
// src/styles/lint.test.ts runs oxlint on this file and checks every marked line.
import { AgentxMark } from "@/components/agentx-mark";
import { Icon } from "@/components/icon";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { KeyHint } from "../../src/components/key-hint";

declare const tone: string;
declare const open: boolean;

export const cases = (
  <>
    <div className="text-[13px]" /> {/* bad(no-arbitrary-values): arbitrary font size */}
    <div className="rounded-[10px]" /> {/* bad(no-arbitrary-values): arbitrary radius */}
    <div className="p-[7px]" /> {/* bad(no-arbitrary-values): arbitrary spacing */}
    <div className="bg-[#fff]" /> {/* bad(no-arbitrary-values): arbitrary colour */}
    <div className="hover:shadow-[0_0_0_1px_red]" /> {/* bad(no-arbitrary-values): arbitrary shadow behind a variant */}
    <div className="[mask-type:luminance]" /> {/* bad(no-arbitrary-values): arbitrary property */}
    <div className="bg-amber-500" /> {/* bad(no-raw-colors): palette colour */}
    <div className="text-slate-400" /> {/* bad(no-raw-colors): palette colour */}
    <div className="border-rose-300" /> {/* bad(no-raw-colors): palette colour */}
    <svg fill="#ff0000" /> {/* bad(no-raw-colors): raw colour in an SVG attribute */}
    <div className="text-sm" /> {/* bad(no-unknown-classes): default font size, not in the theme */}
    <div className="rounded-md" /> {/* bad(no-unknown-classes): default radius, not in the theme */}
    <div className="shadow-sm" /> {/* bad(no-unknown-classes): default shadow, not in the theme */}
    <div className="tracking-wide" /> {/* bad(no-unknown-classes): default tracking, not in the theme */}
    <div className="bg-card-bgg" /> {/* bad(no-raw-colors): misspelt token */}
    <div className="flex-cols" /> {/* bad(no-unknown-classes): unknown class */}
    <div className={cn("p-4", open && "bg-shell-bgg")} /> {/* bad(no-raw-colors): misspelt token inside cn() */}
    <div style={{ color: "red" }} /> {/* bad(no-inline-styles): inline style */}
    <div style={{ padding: 4, "--x": 1 }} /> {/* bad(no-inline-styles): inline style beside a custom property */}
    <div style={{ "--tint": "#ec4899" }} /> {/* bad(no-inline-styles): raw colour in a custom property */}
    <style>{".x { color: red }"}</style> {/* bad(no-inline-styles): style element */}
    <Button className="w-full" /> {/* bad(no-restyle): className on a custom component, even layout */}
    <Button className="bg-button-primary-bg" /> {/* bad(no-restyle): className on a custom component */}
    <Icon name="search" className="text-text-muted" /> {/* bad(no-restyle): className on a custom component */}
    <KeyHint className="p-1">esc</KeyHint> {/* bad(no-restyle): className on a component from a relative import */}
    <AgentxMark size={26} className={cn("rounded-shell-logo")} /> {/* bad(no-restyle): className through cn() */}
    <Button className={tone} /> {/* bad(require-static-classes): dynamic class string */}
    <Button className={`bg-${tone}`} /> {/* bad(require-static-classes): dynamic class string */}
    <Button style={{ "--x": 1 }} /> {/* bad(forbid-component-props): style on a component */}
    <button type="button" /> {/* bad(forbid-elements): native control outside src/components */}
    <input /> {/* bad(forbid-elements): native control outside src/components */}
    <textarea /> {/* bad(forbid-elements): native control outside src/components */}
    <select /> {/* bad(forbid-elements): native control outside src/components */}
    <div className="bg-card-bg text-text-primary rounded-card" /> {/* ok: tokens */}
    <div className="type-body gap-2.5 pb-5.5 w-(--shell-sidebar-width)" /> {/* ok: role, 2px grid, layout token */}
    <div className="left-1/2 -translate-x-1/2 data-[selected=true]:bg-row-hover" /> {/* ok: fractions, data variant */}
    <div className={cn("flex", open && "hidden")} /> {/* ok: static classes through cn() */}
    <div style={{ "--progress": 1 }} /> {/* ok: runtime value as a custom property */}
    <Button variant="primary" size="sm" /> {/* ok: typed props */}
    <Icon name="search" size={14} /> {/* ok: typed props */}
  </>
);
