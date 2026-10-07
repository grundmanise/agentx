// A Button given a className the types reject, as untyped code could still pass one.
/* oxlint-disable shadcn/no-restyle */
import { Button } from "#/components/ui/button.tsx";

import { mount } from "./mount.tsx";

const forced = { className: "mt-2" } as object;

mount(
  <Button variant="primary" {...forced}>
    Save
  </Button>
);
