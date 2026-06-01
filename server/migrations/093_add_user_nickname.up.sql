ALTER TABLE "user" ADD COLUMN nickname TEXT;

UPDATE "user" SET nickname = '牧思' WHERE email = '373719@alibaba-inc.com';
UPDATE "user" SET nickname = '小蓝' WHERE email = '270401@alibaba-inc.com';
