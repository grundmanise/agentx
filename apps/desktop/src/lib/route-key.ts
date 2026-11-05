// The key a screen's route names: `/skills/<key>`, `/servers/<key>`. The screens whose selection
// or scroll position is the route read it here, so another screen can open an item by its path.
import { useParams } from "react-router";

/** The one segment after the screen's path, decoded; undefined when there is none or more than one. */
export const useRouteKey = (): string | undefined => {
  const splat = useParams()["*"] ?? "";
  if (splat === "" || splat.includes("/")) {
    return undefined;
  }
  try {
    return decodeURIComponent(splat);
  } catch {
    return splat;
  }
};
