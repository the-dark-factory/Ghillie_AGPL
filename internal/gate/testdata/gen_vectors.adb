--  gen_vectors.adb — scratch harness that prints the COMPLETE decision table of
--  the proven core Facade_Command_Pkg (ada-factory ledger 112).
--
--  This is a TEST HARNESS, NOT A FACTORY COMPONENT. It is SPARK_Mode (Off) and
--  contains no protocol decisions of its own: it only enumerates the input
--  domain and asks May_Command. Every verdict in the output is the proven
--  core's, never this file's.
--
--  The domain is finite and small — Command_Type (6) x ceiling (6) x
--  Consent_Type (3) x Authentic (2) = 216 — so the emitted table is EXHAUSTIVE.
--  There is no input to May_Command that these vectors omit.
--
--  Output: one line per case, "c ceiling k authentic verdict", all fields the
--  integer 'Pos of the enumeration (verdict/authentic 0 = False, 1 = True).
--
--  Regeneration procedure: see README.md beside this file.
pragma SPARK_Mode (Off);

with Ada.Text_IO;         use Ada.Text_IO;
with Facade_Command_Pkg;  use Facade_Command_Pkg;

procedure Gen_Vectors is

   function Bit (X : Boolean) return String is (if X then "1" else "0");

   function Pos (N : Natural) return String is
      S : constant String := Natural'Image (N);
   begin
      return S (S'First + 1 .. S'Last);   --  strip the leading blank
   end Pos;

begin
   for C in Command_Type loop
      for Ceiling in Command_Type loop
         for K in Consent_Type loop
            for A in Boolean loop
               Put_Line
                 (Pos (Command_Type'Pos (C))       & " " &
                  Pos (Command_Type'Pos (Ceiling)) & " " &
                  Pos (Consent_Type'Pos (K))       & " " &
                  Bit (A)                          & " " &
                  Bit (May_Command (C, Ceiling, K, A)));
            end loop;
         end loop;
      end loop;
   end loop;
end Gen_Vectors;
