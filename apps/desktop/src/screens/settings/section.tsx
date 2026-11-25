// One card of the Settings page (DESIGN.md §6 "Settings"), on shadcn's Card: its title, then its
// rows, a hairline between two of them. Cards stack in the order `settings-screen.tsx` lists them;
// a card that has nothing to show renders nothing.
//
// A row (`SettingsRow`), on shadcn's Item, is the setting's name over its line at the left and its
// control at the right; what shows under the row, such as a field or a result, comes on a line of
// its own. A row with no name is only what it holds, such as the Editor's radio group. With
// `htmlFor`, the whole row labels that control, so a click anywhere on it toggles a switch.
import { useId } from "react";
import type { ReactNode } from "react";

import { Card, CardContent, CardTitle } from "#/components/ui/card.tsx";
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemFooter,
  ItemTitle,
} from "#/components/ui/item.tsx";

export interface SettingsCardProps {
  /** The card's title, such as "This machine". */
  title: string;
  children: ReactNode;
}

export const SettingsCard = ({ title, children }: SettingsCardProps) => {
  const id = useId();
  return (
    <Card aria-labelledby={id}>
      <CardTitle id={id}>{title}</CardTitle>
      <CardContent>{children}</CardContent>
    </Card>
  );
};

export interface SettingsRowProps {
  /** The setting's name. */
  name?: ReactNode;
  /** The line under the name. */
  line?: ReactNode;
  /** The line's id, for the control's `aria-describedby`. */
  lineId?: string;
  /** The control at the right: a switch, buttons, a value. */
  control?: ReactNode;
  /** The id of the control the row labels. */
  htmlFor?: string;
  /** What shows under the row. */
  children?: ReactNode;
}

export const SettingsRow = ({
  name,
  line,
  lineId,
  control,
  htmlFor,
  children,
}: SettingsRowProps) => {
  if (name === undefined) {
    return <Item>{children}</Item>;
  }
  const parts = (
    <>
      <ItemContent>
        <ItemTitle>{name}</ItemTitle>
        {line !== undefined && (
          <ItemDescription id={lineId}>{line}</ItemDescription>
        )}
      </ItemContent>
      {control !== undefined && <ItemActions>{control}</ItemActions>}
      {children !== undefined && children !== false && (
        <ItemFooter>{children}</ItemFooter>
      )}
    </>
  );
  return htmlFor === undefined ? (
    <Item>{parts}</Item>
  ) : (
    <Item asChild>
      <label htmlFor={htmlFor} className="cursor-pointer">
        {parts}
      </label>
    </Item>
  );
};
