// Classes and markup the design-system lint must reject (`bad`) or accept (`ok`), one per
// line. src/styles/lint.test.ts runs oxlint on this file and checks every marked line.
export const cases = (
  <>
    <div className="text-[13px]" /> {/* bad: arbitrary font size */}
    <div className="rounded-[10px]" /> {/* bad: arbitrary radius */}
    <div className="p-[7px]" /> {/* bad: arbitrary spacing */}
    <div className="bg-[#fff]" /> {/* bad: arbitrary colour */}
    <div className="hover:shadow-[0_0_0_1px_red]" /> {/* bad: arbitrary shadow behind a variant */}
    <div className="[mask-type:luminance]" /> {/* bad: arbitrary property */}
    <div className="gap-2.25" /> {/* bad: quarter step */}
    <div className="text-sm" /> {/* bad: default font size */}
    <div className="font-mono" /> {/* bad: face outside a type role */}
    <div className="leading-none" /> {/* bad: line height outside a type role */}
    <div className="tracking-wide" /> {/* bad: tracking outside a type role */}
    <div className="rounded-full" /> {/* bad: radius outside the tokens */}
    <div className="rounded-md" /> {/* bad: default radius */}
    <div className="shadow-sm" /> {/* bad: default shadow */}
    <div className="bg-amber-500" /> {/* bad: default colour with a shade */}
    <div className="text-slate-400" /> {/* bad: default colour with a shade */}
    <div className="border-rose-300" /> {/* bad: default colour with a shade */}
    <div className="bg-white" /> {/* bad: default white */}
    <div className="text-black" /> {/* bad: default black */}
    <div className="bg-card-bg/50" /> {/* bad: alpha on a colour token */}
    <div className="bg-card-bgg" /> {/* bad: misspelt token */}
    <div style={{ color: "red" }} /> {/* bad: inline style */}
    <div style={{ padding: 4, "--x": 1 }} /> {/* bad: inline style beside a custom property */}
    <button type="button" /> {/* bad: native control outside src/components */}
    <input /> {/* bad: native control outside src/components */}
    <div className="bg-card-bg text-text-primary rounded-card" /> {/* ok: tokens */}
    <div className="type-body gap-2.5 pb-5.5 w-(--shell-sidebar-width)" /> {/* ok: role, 2px grid, layout token */}
    <div className="left-1/2 -translate-x-1/2 data-[selected=true]:bg-row-hover" /> {/* ok: fractions, data variant */}
    <div style={{ "--progress": 1 }} /> {/* ok: runtime value as a custom property */}
  </>
);
