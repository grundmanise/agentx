import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { App } from "#/app/app.tsx";

export function renderApp(path = "/inbox") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}
