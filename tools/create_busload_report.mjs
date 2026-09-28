import fs from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";

const artifactModule = process.env.ARTIFACT_TOOL_MODULE;
if (!artifactModule) {
  throw new Error("ARTIFACT_TOOL_MODULE must point to @oai/artifact-tool/dist/artifact_tool.mjs");
}
const { FileBlob, SpreadsheetFile, Workbook } = await import(pathToFileURL(artifactModule).href);

const outputDir = path.resolve(process.argv[2] ?? "output/busload_calibration/final_20260928_1131");
const priorDir = process.argv[3] ? path.resolve(process.argv[3]) : null;
const summaryPath = path.join(outputDir, "summary.csv");
const rawPath = path.join(outputDir, "raw_samples.csv");
const metadataPath = path.join(outputDir, "metadata.json");

function parseSimpleCSV(text) {
  return text.trim().split(/\r?\n/).map((line) => line.split(","));
}

function number(value) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) throw new Error(`invalid numeric CSV value: ${value}`);
  return parsed;
}

const summaryCSV = parseSimpleCSV(await fs.readFile(summaryPath, "utf8"));
const rawCSV = parseSimpleCSV(await fs.readFile(rawPath, "utf8"));
const metadata = JSON.parse(await fs.readFile(metadataPath, "utf8"));
const priorCSV = priorDir ? parseSimpleCSV(await fs.readFile(path.join(priorDir, "summary.csv"), "utf8")) : null;
const priorMetadata = priorDir ? JSON.parse(await fs.readFile(path.join(priorDir, "metadata.json"), "utf8")) : null;
const comparisonName = metadata.comparison_device ?? "Toomoss";
const comparisonKey = comparisonName.toLowerCase();
const stressIDs = metadata.stress_can_ids ?? [metadata.stress_can_id];
const headers = summaryCSV[0];
const summaryRecords = summaryCSV.slice(1).map((row) => Object.fromEntries(headers.map((header, index) => [header, row[index]])));
const priorRecords = priorCSV?.slice(1).map((row) => Object.fromEntries(priorCSV[0].map((header, index) => [header, row[index]])));
if (priorMetadata && (
  priorMetadata.status !== "complete" || metadata.status !== "complete" ||
  priorMetadata.stress_frame_format !== metadata.stress_frame_format ||
  priorMetadata.payload_length_bytes !== metadata.payload_length_bytes ||
  priorMetadata.nominal_bitrate_bps !== metadata.nominal_bitrate_bps ||
  priorMetadata.data_bitrate_bps !== metadata.data_bitrate_bps ||
  priorMetadata.comparison_device !== metadata.comparison_device ||
  priorRecords.length !== summaryRecords.length
)) {
  throw new Error("comparison runs must have matching completed frame, bitrate, observer, and target configurations");
}

const workbook = Workbook.create();
const summary = workbook.worksheets.add("Summary");
const comparison = priorDir ? workbook.worksheets.add("ID Comparison") : null;
const raw = workbook.worksheets.add("Raw Data");
const protocol = workbook.worksheets.add("Protocol");
const fontFamily = "Arial";
const navy = "#17365D";
const blue = "#2F75B5";
const orange = "#ED7D31";
const red = "#C00000";
const lightBlue = "#D9EAF7";
const lightGray = "#F2F4F7";
const utcPlus8Milliseconds = 8 * 60 * 60 * 1000;
const asUTCPlus8ExcelDate = (value) => new Date(new Date(value).getTime() + utcPlus8Milliseconds);

summary.showGridLines = false;
summary.tabColor = navy;
summary.getRange("A2:O2").format.font = { name: fontFamily, size: 14, bold: true, color: "#1F2937" };
summary.getRange("A2").values = [[`CAN bus-load calibration: TSMaster vs ${comparisonName} — ${metadata.stress_frame_format}`]];
summary.getRange("A3:F3").values = [["Max |bias| (pp)", null, "Mean |bias| (pp)", null, "Min bias (pp)", null]];
summary.getRange("A3:F3").format.font = { name: fontFamily, size: 10, bold: true, color: "#374151" };

const summaryHeaders = [
  "Target (%)", "Command (fps)", "Samples", "TSMaster mean (%)", `${comparisonName} mean (%)`, "Bias (pp)",
  "Relative error", "TSMaster SD (pp)", `${comparisonName} SD (pp)`, "TSMaster min (%)", "TSMaster max (%)",
  `${comparisonName} min (%)`, `${comparisonName} max (%)`, "TSMaster drops", `${comparisonName} drops`,
];
summary.getRange("A5:O5").values = [summaryHeaders];
const summaryRows = summaryRecords.map((record) => [
  number(record.target_pct), number(record.command_fps), number(record.samples), number(record.tsmaster_mean_pct),
  number(record[`${comparisonKey}_mean_pct`]), null, null, number(record.tsmaster_stddev_pct), number(record[`${comparisonKey}_stddev_pct`]),
  number(record.tsmaster_min_pct), number(record.tsmaster_max_pct), number(record[`${comparisonKey}_min_pct`]), number(record[`${comparisonKey}_max_pct`]),
  number(record.tsmaster_source_dropped), number(record[`${comparisonKey}_source_dropped`]),
]);
summary.getRange(`A6:O${5 + summaryRows.length}`).values = summaryRows;
summary.getRange("F6").formulas = [["=E6-D6"]];
summary.getRange(`F6:F${5 + summaryRows.length}`).fillDown();
summary.getRange("G6").formulas = [["=IF(D6=0,\"\",F6/D6)"]];
summary.getRange(`G6:G${5 + summaryRows.length}`).fillDown();
summary.getRange("B3").formulas = [[`=MAX(MAX(F6:F${5 + summaryRows.length}),-MIN(F6:F${5 + summaryRows.length}))`]];
const absoluteBiasTerms = Array.from({ length: summaryRows.length }, (_, index) => `ABS(F${6 + index})`).join("+");
summary.getRange("D3").formulas = [[`=(${absoluteBiasTerms})/COUNT(F6:F${5 + summaryRows.length})`]];
summary.getRange("F3").formulas = [[`=MIN(F6:F${5 + summaryRows.length})`]];

summary.getRange("A5:O5").format = {
  fill: navy,
  font: { name: fontFamily, size: 10, bold: true, color: "#FFFFFF" },
  horizontalAlignment: "center",
  verticalAlignment: "center",
  wrapText: true,
  borders: { preset: "inside", style: "thin", color: "#FFFFFF" },
};
summary.getRange(`A6:O${5 + summaryRows.length}`).format.font = { name: fontFamily, size: 10, color: "#1F2937" };
summary.getRange(`A6:C${5 + summaryRows.length}`).format.numberFormat = "0.00";
summary.getRange(`D6:F${5 + summaryRows.length}`).format.numberFormat = "0.000";
summary.getRange(`G6:G${5 + summaryRows.length}`).format.numberFormat = "0.00%";
summary.getRange(`H6:M${5 + summaryRows.length}`).format.numberFormat = "0.000";
summary.getRange(`N6:O${5 + summaryRows.length}`).format.numberFormat = "0";
for (const address of ["B3", "D3", "F3"]) {
  summary.getRange(address).format.numberFormat = "0.000";
  summary.getRange(address).format = { fill: lightBlue, font: { name: fontFamily, size: 11, bold: true, color: navy }, horizontalAlignment: "center" };
}
summary.tables.add(`A5:O${5 + summaryRows.length}`, true, "BusLoadSummary").style = "TableStyleMedium2";
summary.freezePanes.freezeRows(5);

const comparisonChart = summary.charts.add("line", [
  summary.getRange(`A5:A${5 + summaryRows.length}`),
  summary.getRange(`D5:D${5 + summaryRows.length}`),
  summary.getRange(`E5:E${5 + summaryRows.length}`),
]);
comparisonChart.title = "Measured bus load (%)";
comparisonChart.titleTextStyle.fontSize = 12;
comparisonChart.titleTextStyle.typeface = fontFamily;
comparisonChart.legend = { position: "top", textStyle: { typeface: fontFamily, fontSize: 10 } };
comparisonChart.xAxis = { axisType: "textAxis", textStyle: { typeface: fontFamily, fontSize: 9 } };
comparisonChart.yAxis = { numberFormatCode: "0.0", numberFormatSourceLinked: false, textStyle: { typeface: fontFamily, fontSize: 9 } };
comparisonChart.xAxis.title.text = "Target load (%)";
comparisonChart.yAxis.title.text = "Measured load (%)";
comparisonChart.setPosition("Q3", "Y17");
comparisonChart.series.items[0].line = { fill: blue, style: "solid", width: 2 };
comparisonChart.series.items[1].line = { fill: orange, style: "solid", width: 2 };

const biasChart = summary.charts.add("line", [
  summary.getRange(`A5:A${5 + summaryRows.length}`),
  summary.getRange(`F5:F${5 + summaryRows.length}`),
]);
biasChart.title = `${comparisonName} bias vs TSMaster (percentage points)`;
biasChart.titleTextStyle.fontSize = 12;
biasChart.titleTextStyle.typeface = fontFamily;
biasChart.hasLegend = false;
biasChart.xAxis = { axisType: "textAxis", textStyle: { typeface: fontFamily, fontSize: 9 } };
biasChart.yAxis = { numberFormatCode: "0.00", numberFormatSourceLinked: false, textStyle: { typeface: fontFamily, fontSize: 9 } };
biasChart.xAxis.title.text = "Target load (%)";
biasChart.yAxis.title.text = `${comparisonName} - TSMaster (pp)`;
biasChart.setPosition("Q19", "Y33");
biasChart.series.items[0].line = { fill: red, style: "solid", width: 2 };

summary.getRange("A:O").format.autofitColumns();
summary.getRange("A:A").format.columnWidth = 12;
summary.getRange("B:B").format.columnWidth = 14;
summary.getRange("C:C").format.columnWidth = 10;
summary.getRange("D:M").format.columnWidth = 16;
summary.getRange("N:O").format.columnWidth = 15;
summary.getRange("5:5").format.rowHeight = 34;

if (comparison) {
  const priorIDs = priorMetadata.stress_can_ids ?? [priorMetadata.stress_can_id];
  comparison.showGridLines = false;
  comparison.tabColor = blue;
  comparison.getRange("A2:Q2").format.font = { name: fontFamily, size: 14, bold: true, color: "#1F2937" };
  comparison.getRange("A2").values = [["Classic CAN load bias by transmitted ID set"]];
  comparison.getRange("A3").values = [[`${priorIDs.join(", ")} versus ${stressIDs.join(", ")}`]];
  comparison.getRange("A3:Q3").format.font = { name: fontFamily, size: 10, color: "#374151" };
  comparison.getRange("A5:G5").values = [[
    "Target (%)", "Single-ID TSMaster (%)", "Single-ID PCAN (%)", "Single-ID bias (pp)",
    "Multi-ID TSMaster (%)", "Multi-ID PCAN (%)", "Multi-ID bias (pp)",
  ]];
  const comparisonRows = summaryRecords.map((current, index) => {
    const prior = priorRecords[index];
    const target = number(current.target_pct);
    if (number(prior.target_pct) !== target) throw new Error(`target mismatch at row ${index + 1}`);
    return [
      target, number(prior.tsmaster_mean_pct), number(prior[`${comparisonKey}_mean_pct`]), null,
      number(current.tsmaster_mean_pct), number(current[`${comparisonKey}_mean_pct`]), null,
    ];
  });
  const lastRow = 5 + comparisonRows.length;
  comparison.getRange(`A6:G${lastRow}`).values = comparisonRows;
  comparison.getRange("D6").formulas = [["=C6-B6"]];
  comparison.getRange(`D6:D${lastRow}`).fillDown();
  comparison.getRange("G6").formulas = [["=F6-E6"]];
  comparison.getRange(`G6:G${lastRow}`).fillDown();
  comparison.getRange("A5:G5").format = {
    fill: navy, font: { name: fontFamily, size: 10, bold: true, color: "#FFFFFF" },
    horizontalAlignment: "center", verticalAlignment: "center", wrapText: true,
    borders: { preset: "inside", style: "thin", color: "#FFFFFF" },
  };
  comparison.getRange(`A6:G${lastRow}`).format.font = { name: fontFamily, size: 10, color: "#1F2937" };
  comparison.getRange(`A6:G${lastRow}`).format.numberFormat = "0.000";
  comparison.tables.add(`A5:G${lastRow}`, true, "BusLoadIDComparison").style = "TableStyleMedium2";
  comparison.getRange("A:A").format.columnWidth = 12;
  comparison.getRange("B:G").format.columnWidth = 20;
  comparison.getRange("5:5").format.rowHeight = 34;
  comparison.freezePanes.freezeRows(5);

  const idChart = comparison.charts.add("line", [
    comparison.getRange(`A5:A${lastRow}`), comparison.getRange(`D5:D${lastRow}`), comparison.getRange(`G5:G${lastRow}`),
  ]);
  idChart.title = "PCAN bias versus TSMaster (pp)";
  idChart.titleTextStyle.fontSize = 12;
  idChart.titleTextStyle.typeface = fontFamily;
  idChart.legend = { position: "top", textStyle: { typeface: fontFamily, fontSize: 10 } };
  idChart.xAxis = { axisType: "textAxis", textStyle: { typeface: fontFamily, fontSize: 9 } };
  idChart.yAxis = { numberFormatCode: "0.00", numberFormatSourceLinked: false, textStyle: { typeface: fontFamily, fontSize: 9 } };
  idChart.xAxis.title.text = "Target load (%)";
  idChart.yAxis.title.text = "Bias (pp)";
  idChart.setPosition("I5", "Q21");
  idChart.series.items[0].line = { fill: blue, style: "solid", width: 2 };
  idChart.series.items[1].line = { fill: orange, style: "solid", width: 2 };
}

raw.showGridLines = false;
raw.tabColor = "#9EADBA";
const rawHeaders = rawCSV[0];
rawHeaders[0] = "timestamp_local_utc+08";
const rawRows = rawCSV.slice(1).map((row) => row.map((value, index) => {
  if (index === 0) return asUTCPlus8ExcelDate(value);
  if (index === 2) return value;
  return number(value);
}));
const rawColumnIndex = Object.fromEntries(rawCSV[0].map((header, index) => [header, index]));
const rawMaximum = (header) => Math.max(...rawRows.map((row) => Number(row[rawColumnIndex[header]])));
raw.getRange("A1:Q1").values = [rawHeaders];
raw.getRange(`A2:Q${1 + rawRows.length}`).values = rawRows;
raw.getRange("A1:Q1").format = {
  fill: navy,
  font: { name: fontFamily, size: 10, bold: true, color: "#FFFFFF" },
  horizontalAlignment: "center",
  verticalAlignment: "center",
  wrapText: true,
  borders: { preset: "inside", style: "thin", color: "#FFFFFF" },
};
raw.getRange(`A2:Q${1 + rawRows.length}`).format.font = { name: fontFamily, size: 9, color: "#1F2937" };
raw.getRange(`A2:A${1 + rawRows.length}`).format.numberFormat = "yyyy-mm-dd hh:mm:ss.000";
raw.getRange(`B2:B${1 + rawRows.length}`).format.numberFormat = "0.000";
raw.getRange(`D2:I${1 + rawRows.length}`).format.numberFormat = "0.000000";
raw.getRange(`J2:Q${1 + rawRows.length}`).format.numberFormat = "0";
raw.tables.add(`A1:Q${1 + rawRows.length}`, true, "BusLoadRawSamples").style = "TableStyleMedium2";
raw.freezePanes.freezeRows(1);
raw.freezePanes.freezeColumns(3);
raw.getRange("A:Q").format.autofitColumns();
raw.getRange("A:A").format.columnWidth = 28;
raw.getRange("C:C").format.columnWidth = 12;
raw.getRange("D:I").format.columnWidth = 16;
raw.getRange("J:Q").format.columnWidth = 18;
raw.getRange("1:1").format.rowHeight = 36;

protocol.showGridLines = false;
protocol.tabColor = "#A5A5A5";
protocol.getRange("A2:B2").format.font = { name: fontFamily, size: 14, bold: true, color: "#1F2937" };
protocol.getRange("A2").values = [["Measurement protocol and provenance"]];
const protocolRows = [
  ["Run status", metadata.status],
  ["Started at (UTC+08)", asUTCPlus8ExcelDate(metadata.started_at)],
  ["Finished at (UTC+08)", asUTCPlus8ExcelDate(metadata.finished_at)],
  ["CAN channel", `CAN${metadata.channel_zero_based + 1}`],
  ["Nominal bitrate", metadata.nominal_bitrate_bps],
  ["Data bitrate", metadata.data_bitrate_bps === 0 ? "not used (classic CAN)" : metadata.data_bitrate_bps],
  ["Stress frame", `${metadata.stress_frame_format}, IDs ${stressIDs.join(", ")}, ${metadata.payload_length_bytes} data bytes`],
  ["BRS enabled", metadata.stress_brs ? "yes" : "no"],
  ["Payload pattern", metadata.payload_pattern ?? "not recorded"],
  ["Target sequence (%)", metadata.targets_pct.join(", ")],
  ["Warm-up", metadata.warmup],
  ["Settle per rate change", metadata.settle],
  ["Sampling", `${metadata.samples_per_point} samples per target, ${metadata.sample_interval} interval`],
  ["Reference", "TSMaster tsapp_get_bus_statistics (native hardware/driver statistic)"],
  ["Compared result", `${comparisonName} rolling one-second software estimate from received frames`],
  ["Traffic control", `TSMaster transmitted ${metadata.stress_frame_format} across IDs ${stressIDs.join(", ")} in round-robin order; feedback tuned from reference load`],
  ["Integrity check", `${rawRows.length} retained samples; ${rawMaximum("send_errors")} send errors; ${rawMaximum("tsmaster_source_dropped")} TSMaster source drops; ${rawMaximum(`${comparisonKey}_source_dropped`)} ${comparisonName} source drops`],
  ["Raw source", rawPath],
  ["Summary source", summaryPath],
];
protocol.getRange("A5:B5").values = [["Field", "Value"]];
protocol.getRange(`A6:B${5 + protocolRows.length}`).values = protocolRows;
protocol.getRange("A5:B5").format = { fill: navy, font: { name: fontFamily, size: 10, bold: true, color: "#FFFFFF" } };
protocol.getRange(`A6:B${5 + protocolRows.length}`).format.font = { name: fontFamily, size: 10, color: "#1F2937" };
protocol.getRange(`A6:A${5 + protocolRows.length}`).format = { fill: lightGray, font: { name: fontFamily, size: 10, bold: true, color: "#374151" } };
protocol.getRange(`B6:B${5 + protocolRows.length}`).format.wrapText = true;
protocol.getRange("B7:B8").format.numberFormat = "yyyy-mm-dd hh:mm:ss";
protocol.getRange("A:A").format.columnWidth = 27;
protocol.getRange("B:B").format.columnWidth = 96;
protocol.getRange("5:5").format.rowHeight = 24;

workbook.recalculate();

const inspectSummary = await workbook.inspect({ kind: "region", sheetId: "Summary", range: `A2:O${5 + summaryRows.length}`, maxChars: 7000 });
const inspectComparison = comparison ? await workbook.inspect({ kind: "region", sheetId: "ID Comparison", range: `A2:G${5 + summaryRows.length}`, maxChars: 3500 }) : null;
const inspectRaw = await workbook.inspect({ kind: "region", sheetId: "Raw Data", range: "A1:Q8", maxChars: 5000 });
const inspectProtocol = await workbook.inspect({ kind: "region", sheetId: "Protocol", range: `A2:B${5 + protocolRows.length}`, maxChars: 5000 });
const formulaErrors = await workbook.inspect({
  kind: "match",
  searchTerm: "#REF!|#DIV/0!|#VALUE!|#NAME\\?|#N/A|#NUM!|#NULL!|#SPILL!|#CALC!",
  options: { useRegex: true, maxResults: 300 },
  summary: "final formula error scan",
});
console.log(inspectSummary.ndjson);
if (inspectComparison) console.log(inspectComparison.ndjson);
console.log(inspectRaw.ndjson);
console.log(inspectProtocol.ndjson);
console.log(formulaErrors.ndjson);

const previews = [["Summary", "summary_preview.png"], ["Raw Data", "raw_data_preview.png"], ["Protocol", "protocol_preview.png"]];
if (comparison) previews.push(["ID Comparison", "id_comparison_preview.png"]);
for (const [sheetName, fileName] of previews) {
  const preview = await workbook.render({ sheetName, autoCrop: "all", scale: 1, format: "png" });
  await fs.writeFile(path.join(outputDir, fileName), new Uint8Array(await preview.arrayBuffer()));
}

const workbookPath = path.join(outputDir, "busload_calibration.xlsx");
const output = await SpreadsheetFile.exportXlsx(workbook);
await output.save(workbookPath);

const imported = await SpreadsheetFile.importXlsx(await FileBlob.load(workbookPath));
const verification = await imported.inspect({ kind: "workbook,sheet,table,drawing", maxChars: 8000, tableMaxRows: 4, tableMaxCols: 8 });
console.log(verification.ndjson);
console.log(JSON.stringify({ workbookPath, summaryRows: summaryRows.length, rawRows: rawRows.length, summaryCharts: summary.charts.items.length, comparisonCharts: comparison?.charts.items.length ?? 0 }));
