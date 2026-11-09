import { allISODates, buildCalendar, tooltip } from "./calendar";

// Wednesday 5 Nov 2025, local time.
const NOW = new Date(2025, 10, 5, 12, 0, 0);
const g = (key: string, count: number) => ({ key, label: key, count });
const cellOf = (cal: ReturnType<typeof buildCalendar>, date: string) => cal.columns.flat().find((c) => c.date === date)!;

describe("buildCalendar", () => {
  it("defaults to 26 weeks, clamps to 1..53 and starts on a Monday", () => {
    expect(buildCalendar([], { now: NOW }).columns).toHaveLength(26);
    expect(buildCalendar([], { now: NOW, weeks: 0 }).columns).toHaveLength(1);
    expect(buildCalendar([], { now: NOW, weeks: 99 }).columns).toHaveLength(53);
    const cal = buildCalendar([], { now: NOW, weeks: 4 });
    expect(cal.columns).toHaveLength(4);
    expect(cal.columns.every((c) => c.length === 7)).toBe(true);
    expect(cal.columns[0][0].date).toBe("2025-10-13");
    expect(cal.columns[3][0].date).toBe("2025-11-03");
  });

  it("puts Monday on row 0, Sunday on row 6 and marks days after today as future", () => {
    const cal = buildCalendar([], { now: NOW, weeks: 4 });
    const last = cal.columns[3];
    expect(last[0].date).toBe("2025-11-03");
    expect(last[6].date).toBe("2025-11-09");
    expect(last.map((c) => c.future)).toEqual([false, false, false, true, true, true, true]);
    expect(cal.columns[2].every((c) => !c.future)).toBe(true);
  });

  it("is full when today is a Sunday and has one real day when today is a Monday", () => {
    expect(buildCalendar([], { now: new Date(2025, 10, 9), weeks: 2 }).columns[1].every((c) => !c.future)).toBe(true);
    expect(buildCalendar([], { now: new Date(2025, 10, 3), weeks: 2 }).columns[1].filter((c) => !c.future)).toHaveLength(1);
  });

  it("draws days with no group as empty squares", () => {
    const cal = buildCalendar([g("2025-11-04", 3)], { now: NOW, weeks: 2 });
    expect(cellOf(cal, "2025-11-04")).toMatchObject({ count: 3, hasData: true });
    expect(cellOf(cal, "2025-11-03")).toMatchObject({ count: 0, level: 0, hasData: false });
  });

  it("buckets levels against the window maximum", () => {
    const days = ["2025-10-27", "2025-10-28", "2025-10-29", "2025-10-30", "2025-10-31", "2025-11-01", "2025-11-02", "2025-11-03"];
    const groups = days.map((d, i) => g(d, i + 1));
    const cal = buildCalendar(groups, { now: NOW, weeks: 2 });
    const levels = groups.map((x) => cellOf(cal, x.key).level);
    expect(levels).toEqual([1, 1, 2, 2, 3, 3, 4, 4]);
    expect(cellOf(cal, "2025-11-04").level).toBe(0);
  });

  it("ignores groups outside the window and adds up duplicate keys", () => {
    const cal = buildCalendar([g("2025-01-01", 50), g("2025-11-04", 2), g("2025-11-04", 3), g("2025-11-08", 9)], { now: NOW, weeks: 2 });
    expect(cellOf(cal, "2025-11-04").count).toBe(5);
    expect(cal.total).toBe(5);
    expect(cellOf(cal, "2025-11-08").count).toBe(0);
  });

  it("labels months at the column holding the 1st, first month at column 0", () => {
    const cal = buildCalendar([], { now: NOW, weeks: 4 });
    expect(cal.months).toEqual([{ col: 0, label: "Oct" }, { col: 2, label: "Nov" }]);
    // a label right after the previous one is skipped: Nov 1 sits in column 1 when the window starts on Oct 20
    expect(buildCalendar([], { now: NOW, weeks: 3 }).months).toEqual([{ col: 0, label: "Oct" }]);
    // column 0 holds the 1st itself, so it carries that month
    expect(buildCalendar([], { now: NOW, weeks: 2 }).months).toEqual([{ col: 0, label: "Nov" }]);
    const wide = buildCalendar([], { now: NOW, weeks: 26 }).months;
    expect(wide[0].col).toBe(0);
    expect(wide.map((m) => m.label)).toEqual(["May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov"]);
  });

  it("labels Mon, Wed and Fri only", () => {
    expect(buildCalendar([], { now: NOW }).weekdayLabels).toEqual(["Mon", "", "Wed", "", "Fri", "", ""]);
  });

  it("writes the total line with singular and plural forms", () => {
    const one = buildCalendar([g("2025-11-04", 1)], { now: NOW, weeks: 26 });
    expect(one.total).toBe(1);
    expect(one.totalText).toBe("1 event in the last 26 weeks");
    expect(buildCalendar([g("2025-11-04", 1)], { now: NOW, weeks: 26, unit: "deploys" }).totalText).toBe("1 deploy in the last 26 weeks");
    expect(buildCalendar([g("2025-11-04", 7)], { now: NOW, weeks: 26, unit: "deploys" }).totalText).toBe("7 deploys in the last 26 weeks");
    expect(buildCalendar([], { now: NOW, weeks: 26, unit: "deploys" }).totalText).toBe("0 deploys in the last 26 weeks");
    expect(buildCalendar([g("2025-11-04", 2)], { now: NOW, weeks: 1 }).totalText).toBe("2 events in the last 1 week");
    expect(buildCalendar([g("2025-11-04", 1)], { now: NOW, weeks: 1, unit: "run" }).totalText).toBe("1 run in the last 1 week");
  });

  it("does not count days after today", () => {
    expect(buildCalendar([g("2025-11-07", 4), g("2025-11-05", 1)], { now: NOW, weeks: 2 }).total).toBe(1);
  });
});

describe("tooltip", () => {
  it("reads weekday, day, month and count", () => {
    expect(tooltip({ date: "2025-11-03", count: 4 })).toBe("Mon 3 Nov · 4");
    expect(tooltip({ date: "2025-11-03", count: 4 }, "deploys")).toBe("Mon 3 Nov · 4 deploys");
    expect(tooltip({ date: "2025-11-09", count: 1 }, "deploys")).toBe("Sun 9 Nov · 1 deploy");
    expect(tooltip({ date: "2025-11-03", count: 0 })).toBe("Mon 3 Nov · 0");
    expect(tooltip({ date: "2025-11-03", count: 0 }, "deploys")).toBe("Mon 3 Nov · 0 deploys");
  });
});

describe("allISODates", () => {
  it("accepts only real dates and needs at least one group", () => {
    expect(allISODates([])).toBe(false);
    expect(allISODates([g("2025-11-03", 1), g("2025-02-28", 1)])).toBe(true);
    expect(allISODates([g("2025-02-30", 1)])).toBe(false);
    expect(allISODates([g("00", 1), g("01", 2)])).toBe(false);
    expect(allISODates([g("2025-11-03", 1), g("x", 1)])).toBe(false);
  });
});
