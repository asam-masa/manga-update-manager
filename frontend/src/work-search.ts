// Convert width variants only; whole-string NFKC would also equate symbols such as ① and 1.
export function searchText(value: string): string {
    return value.replace(/[\uFF00-\uFFEF]+/g, (widthVariant) => widthVariant.normalize('NFKC'))
        .replace(/\u3000/g, ' ')
        .normalize('NFC').toLowerCase();
}

export function titleMatches(title: string, query: string): boolean {
    return searchText(title).includes(searchText(query).trim());
}
