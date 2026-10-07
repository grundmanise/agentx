import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { notify } from "#/components/toast.tsx";
import { renderApp } from "#/test/render.tsx";

describe("app shell", () => {
  it("opens on the Inbox and lists the screens in sidebar order", () => {
    renderApp("/");
    const nav = screen.getByRole("navigation", { name: "Main" });
    expect(within(nav).getAllByRole("link").map((l) => l.textContent)).toEqual([
      "Inbox",
      "Skills",
      "Add",
      "Agents",
      "MCP servers",
      "Plugins",
      "Settings",
    ]);
    expect(within(nav).getByRole("link", { name: "Inbox" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("heading", { name: "Inbox" })).toBeInTheDocument();
  });

  it("moves between screens from the sidebar", async () => {
    renderApp();
    await userEvent.click(screen.getByRole("link", { name: "Add" }));
    expect(screen.getByRole("heading", { name: "Add skills" })).toBeInTheDocument();
  });

  it("keeps Skills active on a screen nested under it", () => {
    renderApp("/skills/review/pdf");
    expect(screen.getByRole("link", { name: "Skills" })).toHaveAttribute("aria-current", "page");
  });
});

describe("command palette", () => {
  it("toggles with the keyboard and opens empty and focused", async () => {
    renderApp();
    await userEvent.keyboard("{Meta>}k{/Meta}");
    const input = screen.getByPlaceholderText("Search skills or run a command");
    expect(input).toHaveFocus();
    expect(input).toHaveValue("");
    await userEvent.keyboard("{Meta>}k{/Meta}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("opens from the command field and closes with Escape", async () => {
    renderApp();
    await userEvent.click(screen.getByRole("button", { name: /Search skills or run a command/u }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("filters by substring and runs the active row with Enter", async () => {
    renderApp();
    await userEvent.keyboard("{Control>}k{/Control}");
    await userEvent.keyboard("SERV");
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual(["MCP servers"]);
    await userEvent.keyboard("{Enter}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "MCP servers" })).toBeInTheDocument();
  });

  it("says when nothing matches", async () => {
    renderApp();
    await userEvent.keyboard("{Control>}k{/Control}zzz");
    expect(screen.getByText("No matches")).toBeInTheDocument();
  });
});

describe("toast", () => {
  it("shows one toast at a time", async () => {
    renderApp();
    act(() => {
      notify("Added pdf to Cursor");
      notify("Removed pdf from Cursor", { label: "Undo", onClick: () => {} });
    });
    expect(await screen.findByText("Removed pdf from Cursor")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText("Added pdf to Cursor")).not.toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Undo" })).toBeInTheDocument();
  });
});
