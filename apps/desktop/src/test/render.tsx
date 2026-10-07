import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { App } from "#/app/app.tsx";

export const renderApp = (path = "/inbox") => 
  render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  )
;
