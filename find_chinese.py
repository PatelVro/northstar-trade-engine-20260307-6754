import os
import re

base_dir = r"c:\Users\Hill\Documents\nofx"
go_files = []
for root, dirs, files in os.walk(base_dir):
    for file in files:
        if file.endswith('.go'):
            go_files.append(os.path.join(root, file))

files_with_chinese = []
for fp in go_files:
    try:
        with open(fp, 'r', encoding='utf-8') as f:
            content = f.read()
        if re.search(r'[\u4e00-\u9fff]', content):
            files_with_chinese.append(os.path.relpath(fp, base_dir))
    except Exception:
        pass

print("Files needing translation:")
for f in files_with_chinese:
    print(f)
