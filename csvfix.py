import csv

def remove_company_name_column(input_file: str, output_file: str):
    with open(input_file, 'r', newline='', encoding='utf-8') as infile, \
         open(output_file, 'w', newline='', encoding='utf-8') as outfile:
        
        reader = csv.DictReader(infile)
        if 'Company Name' not in reader.fieldnames:
            raise ValueError("Column 'Company Name' not found in input CSV.")
        
        # Keep all columns except 'Company Name'
        fieldnames = [field for field in reader.fieldnames if field != 'Company Name']
        writer = csv.DictWriter(outfile, fieldnames=fieldnames)
        
        writer.writeheader()
        for row in reader:
            # Remove 'Company Name' field
            del row['Company Name']
            writer.writerow(row)

    print(f"[INFO] Successfully removed 'Company Name' column and saved to {output_file}")

# Example usage:
remove_company_name_column("input.csv", "output.csv")
