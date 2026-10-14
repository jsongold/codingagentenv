// Table is the common input of datadoc: a TSV file read into header + rows.
export type Table = { name: string; header: string[]; rows: string[][] };
