ALTER TABLE communities ADD COLUMN rules text NOT NULL DEFAULT '' CHECK (length(rules)<=1800);
ALTER TABLE communities ADD COLUMN content_guide text NOT NULL DEFAULT '' CHECK (length(content_guide)<=1000);
ALTER TABLE communities ADD COLUMN category text NOT NULL DEFAULT '' CHECK (length(category)<=80);
