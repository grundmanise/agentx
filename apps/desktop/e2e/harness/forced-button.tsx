// A Button given a className and a style the types reject, as untyped code could still pass one.
/* oxlint-disable shadcn/no-restyle, shadcn/no-inline-styles */
import { Button } from "#/components/ui/button.tsx";

import { mount } from "./mount.tsx";

const forced = { className: "mt-2", style: { marginTop: 8 } } as object;

mount(
  <Button variant="primary" {...forced}>
    Save
  </Button>
);
