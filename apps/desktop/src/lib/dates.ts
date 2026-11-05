/** A date the CLI reports (RFC 3339) as "Oct 2", with the year when it is not this year's. */
export const formatDate = (time: string) => {
  const date = new Date(time);
  if (Number.isNaN(date.getTime())) {
    return time;
  }
  const thisYear = date.getFullYear() === new Date().getFullYear();
  return date.toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    ...(thisYear ? {} : { year: "numeric" }),
  });
};
